import { Hono } from "hono";
import { streamSSE } from "hono/streaming";
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import { streamText, stepCountIs } from "ai";
import { listModels, classifyAiError, AiUnavailable, AiMisconfigured } from "../lib/ai-client";
import { buildAgentTools } from "../agent/tools";
import { tickBirthdayGuard } from "../lib/scenarioEngine";
import { getDb } from "../queries/connection";
import { agentMessages } from "@db/schema";

const SYSTEM = `你是「灵犀银行」App 内置的银行智能助理「小犀」。你接入了用户的真实账户系统，可以直接调用工具办理业务。

## 你可以办理的业务
1. 智能转账：按人名/手机号/备注识别收款人，实时转账、创建定时转账、AA 收款拆分
2. 账单分析：月度/年度收支分类统计、异常交易识别
3. 理财：按风险测评推荐与对比产品、风险测评录入、申购/赎回
4. 卡片管理：申请新卡、调整额度、冻结/解冻、挂失、交易限制、开关线上支付
5. 跨场景守护：如「爱人生日守护」——检测生日，当月锁定活期专款，生日前自动订鲜花蛋糕

## 行为准则
- 金额一律用「¥1,234.56」格式；日期用「11月18日」这种中文表达。
- 执行类操作（转账/申购/赎回/挂失/调额）调用工具前，如果金额超过 ¥5,000 或操作不可逆（挂失），先用一句话向用户确认意图要素（收款人、金额、产品），用户确认后再执行。一笔对话里用户已经明确给出的要素不要重复确认。
- 收款人匹配到多个候选时，列出候选让用户选；找不到时提示先在通讯录添加。
- 工具返回「成功: false」时，如实把原因讲给用户，并给出下一步建议。
- 回答风格：银行级克制、准确、简短。办完业务用 1-3 行小结（谁、多少钱、结果），不要复述工具原始 JSON。
- 不要编造任何余额、利率或交易；一切数字以工具返回为准。
- 适当性原则：推荐或申购理财时，风险等级不得超过用户测评结果。

## 上下文
- 当前真实日期由系统时间决定；用户是账户持有人张伟。`;

type ClientMessage = { role: "user" | "assistant"; content: string };

export const agentRoutes = new Hono();

agentRoutes.post("/chat", async (c) => {
  let body: { messages?: Array<{ role: string; content: string }> };
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "请求格式错误" }, 400);
  }
  const history = (body.messages ?? [])
    .filter((m) => (m.role === "user" || m.role === "assistant") && typeof m.content === "string")
    .slice(-20) as Array<{ role: "user" | "assistant"; content: string }>;
  if (history.length === 0 || history[history.length - 1].role !== "user") {
    return c.json({ error: "消息为空" }, 400);
  }
  const lastUserMsg = history[history.length - 1].content;

  // 每次对话前推进跨场景任务（生日月锁定/下单等）
  let scenarioEvents: string[] = [];
  try {
    scenarioEvents = await tickBirthdayGuard();
  } catch {
    scenarioEvents = [];
  }

  const now = new Date();
  const dateCtx = `今天是 ${now.getFullYear()} 年 ${now.getMonth() + 1} 月 ${now.getDate()} 日。`;
  const eventCtx = scenarioEvents.length > 0 ? `\n刚刚有守护任务自动推进：${scenarioEvents.join("；")}。请在开头用一句话告知用户。` : "";

  try {
    const { defaultModelId } = await listModels();
    const provider = createOpenAICompatible({
      name: "kimi-gw",
      baseURL: process.env.KIMI_AGENTGW_BASE_URL!,
      apiKey: process.env.KIMI_AGENTGW_API_KEY!,
      includeUsage: true,
      supportsStructuredOutputs: true,
    });

    const result = streamText({
      model: provider(defaultModelId),
      system: SYSTEM + "\n\n" + dateCtx + eventCtx,
      messages: history,
      tools: buildAgentTools(),
      stopWhen: stepCountIs(8),
    });

    const usedTools: Array<{ name: string; summary: string }> = [];
    let fullText = "";

    return streamSSE(c, async (stream) => {
      let streamId = 0;
      const send = async (event: string, data: unknown) => {
        await stream.writeSSE({ event, data: JSON.stringify(data), id: String(streamId++) });
      };

      try {
        for await (const part of result.fullStream) {
          if (part.type === "text-delta") {
            const text = (part as { text?: string; textDelta?: string }).text ?? (part as { textDelta?: string }).textDelta ?? "";
            if (text) {
              fullText += text;
              await send("delta", { text });
            }
          } else if (part.type === "tool-call") {
            const p = part as { toolName: string; input?: unknown; args?: unknown };
            const input = p.input ?? p.args ?? {};
            usedTools.push({ name: p.toolName, summary: JSON.stringify(input).slice(0, 200) });
            await send("tool", { phase: "call", name: p.toolName, input });
          } else if (part.type === "tool-result") {
            const p = part as { toolName: string; output?: unknown; result?: unknown };
            await send("tool", { phase: "result", name: p.toolName, output: p.output ?? p.result });
          } else if (part.type === "error") {
            await send("error", { message: "模型调用中断，请重试" });
          }
        }
        // 持久化对话
        const db = getDb();
        await db.insert(agentMessages).values({ role: "user", content: lastUserMsg });
        if (fullText) {
          await db.insert(agentMessages).values({
            role: "assistant",
            content: fullText,
            tools: usedTools.length ? JSON.stringify(usedTools) : null,
          });
        }
        await send("done", { tools: usedTools.map((t) => t.name) });
      } catch (err) {
        const mapped = classifyAiError(err);
        await send("fatal", { kind: mapped.name, message: mapped.message });
      }
    });
  } catch (err) {
    const mapped = classifyAiError(err);
    if (mapped instanceof AiUnavailable) {
      return c.json({ error: "AI 额度已耗尽，此功能暂不可用，请联系站点管理员充值后重试", kind: "AiUnavailable" }, 503);
    }
    if (mapped instanceof AiMisconfigured) {
      return c.json({ error: "AI 服务配置异常，请重新部署站点", kind: "AiMisconfigured" }, 500);
    }
    return c.json({ error: mapped.message || "AI 服务暂时不可用，请稍后重试", kind: mapped.name }, 502);
  }
});
