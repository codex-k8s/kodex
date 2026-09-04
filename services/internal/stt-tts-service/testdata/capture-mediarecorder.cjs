// Воспроизводимые контейнеры настоящего MediaRecorder без внешней сети и микрофона.
const fs = require('node:fs/promises');
const path = require('node:path');
const { createRequire } = require('node:module');
const requirePlaywright = createRequire(process.env.STT_PLAYWRIGHT_PACKAGE || path.resolve(__dirname, '../../../staff/control-center/package.json'));
const playwright = requirePlaywright('playwright');

async function main() {
  const output = process.argv[2];
  if (!output || !path.isAbsolute(output)) throw new Error('absolute output directory is required');
  await fs.mkdir(output, { recursive: true });
  const input = await fs.readFile(path.join(__dirname, '1-2-3-4-5.mp3'));
  const manifest = [];
  for (const [engine, mime, extension] of [
    ['chromium', 'audio/webm;codecs=opus', 'webm'],
    ['firefox', 'audio/ogg;codecs=opus', 'ogg'],
    ['webkit', 'audio/mp4', 'mp4'],
  ]) {
    const browser = await playwright[engine].launch({ headless: true, timeout: 15000 });
    let timer;
    try {
      const page = await browser.newPage();
      await page.route('https://stt.test/**', route => route.fulfill({ contentType: 'text/html', body: '<!doctype html><title>STT test</title>' }));
      await page.goto('https://stt.test/');
      const supported = await page.evaluate(mime => typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported(mime), mime);
      if (!supported) {
        manifest.push({ engine, version: browser.version(), status: 'NOT RUN', reason: 'MediaRecorder format unavailable' });
        process.stdout.write(`${engine}: NOT RUN, MediaRecorder format unavailable\n`);
        continue;
      }
      const result = await Promise.race([page.evaluate(async ({ base64, mime }) => {
        if (!MediaRecorder.isTypeSupported(mime)) throw new Error('recording format unsupported');
        const audio = new AudioContext();
        const bytes = Uint8Array.from(atob(base64), character => character.charCodeAt(0));
        const buffer = await audio.decodeAudioData(bytes.buffer);
        const source = audio.createBufferSource();
        source.buffer = buffer;
        const destination = audio.createMediaStreamDestination();
        source.connect(destination);
        const recorder = new MediaRecorder(destination.stream, { mimeType: mime });
        const chunks = [];
        const complete = new Promise((resolve, reject) => {
          recorder.ondataavailable = event => { if (event.data.size) chunks.push(event.data); };
          recorder.onerror = () => reject(new Error('recording failed'));
          recorder.onstop = resolve;
        });
        await audio.resume();
        recorder.start(100);
        source.start();
        await new Promise(resolve => { source.onended = resolve; });
        recorder.stop();
        await complete;
        const data = await new Blob(chunks, { type: mime }).arrayBuffer();
        await audio.close();
        return Array.from(new Uint8Array(data));
      }, { base64: input.toString('base64'), mime }), new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error('recording deadline exceeded')), 15000);
      })]);
      const file = `${engine}.${extension}`;
      await fs.writeFile(path.join(output, file), Buffer.from(result));
      manifest.push({ engine, version: browser.version(), status: 'PASS', mime, file, size: result.length });
      process.stdout.write(`${engine}: captured ${result.length} bytes\n`);
    } finally { clearTimeout(timer); await browser.close(); }
  }
  await fs.writeFile(path.join(output, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
}
main().catch(() => { process.stderr.write('MediaRecorder capture failed\n'); process.exitCode = 1; });
