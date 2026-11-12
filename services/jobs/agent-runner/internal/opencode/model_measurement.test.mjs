import assert from "node:assert/strict";
import test from "node:test";
import plugin from "./tool_identity.mjs";

// Внешняя модель заменена контролируемым fetch только для проверки границ wrapper.
// Отдельный native harness проверяет этот же hook внутри настоящего pinned OpenCode.
async function fixture(api, send, run) {
  const previous = { ...process.env }, previousFetch = globalThis.fetch;
  const reports = [];
  const paths = { CHAT_COMPLETIONS: "/chat/completions", RESPONSES: "/responses", MESSAGES: "/messages" };
  process.env.KODEX_MODEL_TRANSPORT = JSON.stringify({ provider_id: "synthetic", model_id: "tiny", base_url: "https://model.example.invalid/v1", api, route_kind: "DIRECT" });
  process.env.KODEX_MODEL_MEASUREMENT_URL = "http://127.0.0.1:19001/model-measurements";
  process.env.KODEX_MCP_PROXY_TOKEN = "a".repeat(64);
  process.env.KODEX_NATIVE_REVISION = "b".repeat(64);
  globalThis.fetch = async (url, init) => {
    assert.equal(String(url), process.env.KODEX_MODEL_MEASUREMENT_URL);
    assert.equal(init.headers.Authorization, "Bearer " + "a".repeat(64));
    reports.push(JSON.parse(init.body));
    return new Response(null, { status: 204 });
  };
  try {
    const hooks = await plugin();
    const configuration = { provider: { synthetic: { options: { baseURL: "https://model.example.invalid/v1", fetch: send }, models: { tiny: {} } } } };
    await hooks.config(configuration);
    const headers = { headers: {} };
    await hooks["chat.headers"]({agent:"compaction"}, headers);
    const request = () => configuration.provider.synthetic.options.fetch("https://model.example.invalid/v1"+paths[api], { method:"POST", headers:headers.headers, body:JSON.stringify({ model:"tiny", stream:true }) });
    await run(request,reports);
  } finally {
    globalThis.fetch = previousFetch;
    for (const key of ["KODEX_MODEL_TRANSPORT","KODEX_MODEL_MEASUREMENT_URL","KODEX_MCP_PROXY_TOKEN","KODEX_NATIVE_REVISION"]) {
      if (previous[key] === undefined) delete process.env[key]; else process.env[key] = previous[key];
    }
  }
}

for (const [api, first, final] of [
  ["CHAT_COMPLETIONS", {choices:[{delta:{content:"synthetic"}}]}, "[DONE]"],
  ["RESPONSES", {type:"response.function_call_arguments.delta",delta:"{}"}, JSON.stringify({type:"response.completed"})],
  ["MESSAGES", {type:"content_block_delta",delta:{partial_json:"{}"}}, JSON.stringify({type:"message_stop"})],
]) test(`измеряет ${api} без изменения bytes, лишнего чтения и служебных headers`, async () => {
  let pulls=0;
  const content = `data: ${JSON.stringify(first)}\n\ndata: ${final}\n\n`;
  const chunks=["data: {}\n\n",content.slice(0,13),content.slice(13)];
  await fixture(api, async (_url,init) => {
    assert.equal(init.headers.has("x-kodex-model-phase"),false);
    return new Response(new ReadableStream({async pull(controller){
      pulls++;
      if (!chunks.length) {controller.close();return;}
      await new Promise(resolve=>setTimeout(resolve,15));
      controller.enqueue(new TextEncoder().encode(chunks.shift()));
    }},{highWaterMark:0}),{headers:{"content-type":"text/event-stream"}});
  },async(request,reports)=>{
    const response=await request();
    await new Promise(resolve=>setTimeout(resolve,20));
    assert.equal(pulls,0);
    assert.equal(await response.text(),"data: {}\n\n"+content);
    assert.equal(reports.length,1);const value=reports[0];
    assert.equal(value.phase,"COMPACTION");assert.equal(value.completeness,"COMPLETE");
    assert.ok(value.ttft_ms>=40);assert.ok(value.duration_ms>=value.ttft_ms);assert.equal(value.usage,undefined);
  });
});

test("отмена и повторный HTTP attempt имеют отдельные квитанции без выдуманного TTFT",async()=>{
 let cancelled=0;
 await fixture("CHAT_COMPLETIONS",async()=>new Response(new ReadableStream({pull(controller){controller.enqueue(new TextEncoder().encode("data: {}\n\n"));},cancel(){cancelled++;}},{highWaterMark:0}),{headers:{"content-type":"text/event-stream"}}),async(request,reports)=>{
  for(let index=0;index<2;index++){const response=await request();const reader=response.body.getReader();await reader.read();await reader.cancel();}
  assert.equal(cancelled,2);assert.equal(reports.length,2);assert.notEqual(reports[0].ref,reports[1].ref);
  for(const value of reports){assert.equal(value.outcome,"CANCELLED");assert.equal(value.completeness,"PARTIAL");assert.equal(value.ttft_ms,undefined);}
 });
});

test("oversized SSE не становится успешным полным измерением и не меняет исходный поток",async()=>{
 const content="data: "+"x".repeat(1048577)+"\n\ndata: [DONE]\n\n";
 await fixture("CHAT_COMPLETIONS",async()=>new Response(content,{headers:{"content-type":"text/event-stream"}}),async(request,reports)=>{
  assert.equal(await(await request()).text(),content);assert.equal(reports.length,1);
  assert.equal(reports[0].outcome,"FAILED");assert.equal(reports[0].completeness,"PARTIAL");assert.equal(reports[0].ttft_ms,undefined);
 });
});
