package main

import "net/http"

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

const indexHTML = `<!DOCTYPE html>
<html lang="zh-TW">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Gen Workflow API</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0a0a0a; color: #e0e0e0; min-height: 100vh; }
  .container { max-width: 720px; margin: 0 auto; padding: 2rem 1.5rem; }
  h1 { font-size: 1.5rem; font-weight: 600; margin-bottom: 0.25rem; }
  .subtitle { color: #888; font-size: 0.85rem; margin-bottom: 2rem; }
  .card { background: #161616; border: 1px solid #2a2a2a; border-radius: 12px; padding: 1.5rem; margin-bottom: 1.25rem; }
  .card h2 { font-size: 0.95rem; font-weight: 500; color: #aaa; margin-bottom: 1rem; text-transform: uppercase; letter-spacing: 0.05em; }
  label { display: block; font-size: 0.85rem; color: #999; margin-bottom: 0.35rem; }
  input[type="text"], input[type="number"], textarea, select {
    width: 100%; padding: 0.6rem 0.75rem; background: #0e0e0e; border: 1px solid #333; border-radius: 8px;
    color: #e0e0e0; font-size: 0.9rem; outline: none; transition: border-color 0.2s;
  }
  input:focus, textarea:focus, select:focus { border-color: #555; }
  textarea { resize: vertical; min-height: 60px; font-family: inherit; }
  .field { margin-bottom: 1rem; }
  .row { display: flex; gap: 0.75rem; }
  .row > .field { flex: 1; }
  .toggle-row { display: flex; align-items: center; gap: 0.75rem; margin-bottom: 1rem; }
  .toggle { position: relative; width: 40px; height: 22px; cursor: pointer; }
  .toggle input { display: none; }
  .toggle .slider { position: absolute; inset: 0; background: #333; border-radius: 11px; transition: 0.2s; }
  .toggle .slider::before { content: ""; position: absolute; left: 3px; top: 3px; width: 16px; height: 16px; background: #888; border-radius: 50%; transition: 0.2s; }
  .toggle input:checked + .slider { background: #4a6; }
  .toggle input:checked + .slider::before { transform: translateX(18px); background: #fff; }
  .toggle-label { font-size: 0.85rem; color: #ccc; }
  .mask-item { display: flex; gap: 0.5rem; align-items: center; margin-bottom: 0.5rem; }
  .mask-item input { width: 80px; }
  .btn-sm { padding: 0.4rem 0.75rem; font-size: 0.8rem; border: 1px solid #444; background: #1a1a1a; color: #ccc; border-radius: 6px; cursor: pointer; }
  .btn-sm:hover { background: #252525; }
  .btn-rm { background: none; border: none; color: #a55; cursor: pointer; font-size: 1.1rem; padding: 0 0.3rem; }
  .btn-rm:hover { color: #f66; }
  .submit-btn {
    width: 100%; padding: 0.8rem; font-size: 1rem; font-weight: 500; border: none; border-radius: 10px; cursor: pointer;
    background: linear-gradient(135deg, #3a7bd5, #5a3fd5); color: #fff; transition: opacity 0.2s;
  }
  .submit-btn:hover { opacity: 0.9; }
  .submit-btn:disabled { opacity: 0.4; cursor: not-allowed; }

  /* Status */
  .status-box { display: none; }
  .status-box.active { display: block; }
  .status-bar { display: flex; align-items: center; gap: 0.75rem; padding: 1rem; background: #111; border-radius: 8px; }
  .spinner { width: 20px; height: 20px; border: 2px solid #333; border-top-color: #7af; border-radius: 50%; animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .status-text { font-size: 0.9rem; }
  .status-text.ok { color: #6d8; }
  .status-text.err { color: #f66; }

  /* Result */
  .result-box { display: none; margin-top: 1rem; }
  .result-box.active { display: block; }
  .result-box video { width: 100%; border-radius: 8px; background: #000; }
  .result-url { margin-top: 0.75rem; padding: 0.6rem 0.75rem; background: #0e0e0e; border: 1px solid #333; border-radius: 8px; font-size: 0.8rem; word-break: break-all; color: #7af; }
  .result-url a { color: #7af; text-decoration: none; }
  .result-url a:hover { text-decoration: underline; }
</style>
</head>
<body>
<div class="container">
  <h1>Gen Workflow API</h1>
  <p class="subtitle">fal.ai Kling v2.1 Pro — Image to Animated Backdrop</p>

  <!-- Image -->
  <div class="card">
    <h2>Image</h2>
    <div class="field">
      <label for="imageUrl">Image URL</label>
      <input type="text" id="imageUrl" placeholder="https://... or data:image/png;base64,...">
    </div>
  </div>

  <!-- Parameters -->
  <div class="card">
    <h2>Parameters</h2>
    <div class="row">
      <div class="field">
        <label for="duration">Duration</label>
        <select id="duration"><option value="5">5s</option><option value="10">10s</option></select>
      </div>
      <div class="field">
        <label for="speed">Speed</label>
        <input type="number" id="speed" value="1" step="0.1" min="0.1" max="2">
      </div>
      <div class="field">
        <label for="crossfade">Crossfade (s)</label>
        <input type="number" id="crossfade" value="0.5" step="0.1" min="0.1" max="3">
      </div>
    </div>
    <div class="toggle-row">
      <label class="toggle"><input type="checkbox" id="loop"><span class="slider"></span></label>
      <span class="toggle-label">Seamless Loop (ffmpeg xfade)</span>
    </div>
    <div class="field">
      <label for="prompt">Prompt (optional)</label>
      <textarea id="prompt" placeholder="Leave blank for default backdrop prompt..."></textarea>
    </div>
  </div>

  <!-- Mask Regions -->
  <div class="card">
    <h2>Mask Regions <span style="font-size:0.75rem;color:#666;text-transform:none">(white = frozen)</span></h2>
    <div id="maskList"></div>
    <button class="btn-sm" onclick="addMask()">+ Add Region</button>
  </div>

  <!-- Submit -->
  <button class="submit-btn" id="submitBtn" onclick="submit()">Generate Backdrop</button>

  <!-- Status -->
  <div class="status-box" id="statusBox">
    <div class="card">
      <div class="status-bar">
        <div class="spinner" id="statusSpinner"></div>
        <span class="status-text" id="statusText">Submitting...</span>
      </div>
      <div class="result-box" id="resultBox">
        <video id="resultVideo" controls loop autoplay muted></video>
        <div class="result-url" id="resultUrl"></div>
      </div>
    </div>
  </div>
</div>

<script>
function addMask() {
  const list = document.getElementById('maskList');
  const div = document.createElement('div');
  div.className = 'mask-item';
  div.innerHTML = '<input type="number" placeholder="x" class="mx"> <input type="number" placeholder="y" class="my"> <input type="number" placeholder="w" class="mw"> <input type="number" placeholder="h" class="mh"> <button class="btn-rm" onclick="this.parentElement.remove()">×</button>';
  list.appendChild(div);
}

function getMaskRegions() {
  const items = document.querySelectorAll('.mask-item');
  const regions = [];
  items.forEach(item => {
    const x = parseInt(item.querySelector('.mx').value);
    const y = parseInt(item.querySelector('.my').value);
    const w = parseInt(item.querySelector('.mw').value);
    const h = parseInt(item.querySelector('.mh').value);
    if (!isNaN(x) && !isNaN(y) && !isNaN(w) && !isNaN(h)) regions.push({x, y, w, h});
  });
  return regions;
}

async function submit() {
  const btn = document.getElementById('submitBtn');
  const statusBox = document.getElementById('statusBox');
  const statusText = document.getElementById('statusText');
  const statusSpinner = document.getElementById('statusSpinner');
  const resultBox = document.getElementById('resultBox');

  const imageUrl = document.getElementById('imageUrl').value.trim();
  if (!imageUrl) { alert('Please enter an image URL'); return; }

  btn.disabled = true;
  statusBox.classList.add('active');
  resultBox.classList.remove('active');
  statusSpinner.style.display = 'block';
  statusText.className = 'status-text';
  statusText.textContent = 'Submitting...';

  const body = { image_url: imageUrl };
  body.duration = document.getElementById('duration').value;
  const speed = parseFloat(document.getElementById('speed').value);
  if (speed && speed !== 1) body.speed = speed;
  body.loop = document.getElementById('loop').checked;
  body.crossfade = document.getElementById('crossfade').value;
  const prompt = document.getElementById('prompt').value.trim();
  if (prompt) body.prompt = prompt;
  const masks = getMaskRegions();
  if (masks.length) body.mask_regions = masks;

  try {
    const res = await fetch('/api/backdrop', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
    const data = await res.json();
    if (data.error) throw new Error(data.error);
    statusText.textContent = 'Processing... (this may take a few minutes)';
    pollStatus(data.task_id);
  } catch (e) {
    statusSpinner.style.display = 'none';
    statusText.textContent = 'Error: ' + e.message;
    statusText.className = 'status-text err';
    btn.disabled = false;
  }
}

async function pollStatus(taskId) {
  const statusText = document.getElementById('statusText');
  const statusSpinner = document.getElementById('statusSpinner');
  const resultBox = document.getElementById('resultBox');
  const btn = document.getElementById('submitBtn');

  const poll = async () => {
    try {
      const res = await fetch('/api/status/' + taskId);
      const data = await res.json();

      if (data.status === 'completed') {
        statusSpinner.style.display = 'none';
        statusText.textContent = 'Done!';
        statusText.className = 'status-text ok';
        resultBox.classList.add('active');
        document.getElementById('resultVideo').src = data.video_url;
        document.getElementById('resultUrl').innerHTML = '<a href="' + data.video_url + '" target="_blank">' + data.video_url + '</a>';
        btn.disabled = false;
        return;
      }
      if (data.status === 'failed') {
        statusSpinner.style.display = 'none';
        statusText.textContent = 'Failed: ' + data.error;
        statusText.className = 'status-text err';
        btn.disabled = false;
        return;
      }
      statusText.textContent = 'Processing... (polling every 10s)';
      setTimeout(poll, 10000);
    } catch (e) {
      setTimeout(poll, 10000);
    }
  };
  poll();
}
</script>
</body>
</html>`
