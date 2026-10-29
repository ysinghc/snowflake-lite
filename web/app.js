document.addEventListener('DOMContentLoaded', () => {
  // Tab Navigation
  const tabs = document.querySelectorAll('.tab-btn');
  const contents = document.querySelectorAll('.tab-content');

  tabs.forEach(tab => {
    tab.addEventListener('click', () => {
      tabs.forEach(t => t.classList.remove('active'));
      contents.forEach(c => c.classList.remove('active'));
      tab.classList.add('active');
      const target = document.getElementById(tab.dataset.tab);
      if (target) target.classList.add('active');
    });
  });

  // Start Telemetry Update Loop
  fetchMetrics();
  setInterval(fetchMetrics, 2000);

  // Bind Event Listeners
  document.getElementById('btn-generate').addEventListener('click', generateIDs);
  document.getElementById('btn-rebalance').addEventListener('click', triggerRebalance);
  document.getElementById('btn-create-key').addEventListener('click', createAPIKey);
  document.getElementById('btn-block-ip').addEventListener('click', blockIP);
  document.getElementById('snippet-lang').addEventListener('change', updateCodeSnippet);

  updateCodeSnippet();
});

// --- Telemetry & Metrics ---
async function fetchMetrics() {
  try {
    const res = await fetch('/api/v1/admin/metrics');
    if (!res.ok) return;
    const data = await res.json();

    document.getElementById('stat-rps').textContent = formatNumber(data.currentRps || 0);
    document.getElementById('stat-total').textContent = formatNumber(data.totalIdsGenerated || 0);
    document.getElementById('stat-p95').textContent = (data.p95LatencyUs || 0).toFixed(1) + ' µs';
    document.getElementById('stat-nodes').textContent = data.activeNodes || 1;

    renderNodes(data.clusterNodes || []);
    renderSecuritySummary(data);
  } catch (err) {
    console.error('Failed to fetch metrics:', err);
  }
}

function renderNodes(nodes) {
  const container = document.getElementById('node-grid-container');
  if (!container) return;

  container.innerHTML = nodes.map(node => `
    <div class="node-card">
      <div class="node-header">
        <span class="node-title">${escapeHTML(node.podName)}</span>
        <span class="badge ${node.status === 'healthy' ? 'badge-healthy' : 'badge-rebalancing'}">
          ${escapeHTML(node.status)}
        </span>
      </div>
      <div class="metric-bar-container">
        <div class="metric-label">
          <span>Machine ID: ${node.machineId} (DC ${node.datacenterId})</span>
          <span>Minted: ${formatNumber(node.totalIdsMinted || 0)}</span>
        </div>
        <div class="metric-label">
          <span>CPU Load</span>
          <span>${(node.cpuUsagePct || 0).toFixed(1)}%</span>
        </div>
        <div class="progress-track">
          <div class="progress-fill" style="width: ${Math.min(node.cpuUsagePct || 10, 100)}%"></div>
        </div>
      </div>
    </div>
  `).join('');
}

function renderSecuritySummary(data) {
  const blockedEl = document.getElementById('stat-blocked-ips');
  if (blockedEl) blockedEl.textContent = data.blockedIpCount || 0;
}

// --- ID Generation & Decoding ---
async function generateIDs() {
  const format = document.getElementById('gen-format').value;
  const prefix = document.getElementById('gen-prefix').value.trim();
  const count = parseInt(document.getElementById('gen-count').value, 10) || 1;
  const outputConsole = document.getElementById('console-output');

  outputConsole.textContent = 'Minting IDs...';

  try {
    const res = await fetch(`/api/v1/ids/generate?format=${format}&prefix=${prefix}&count=${count}`);
    const data = await res.json();

    if (!res.ok) {
      outputConsole.textContent = `Error [${data.error?.code || 'FAIL'}]: ${data.error?.message || 'Failed'}`;
      return;
    }

    if (data.id) {
      outputConsole.textContent = `[ID MINTED]\nFormat  : ${data.format}\nPod     : ${data.pod} (Machine ${data.machineId})\nResult  : ${data.id}`;
      decodeID(data.id);
    } else if (data.ids) {
      outputConsole.textContent = `[BATCH MINTED: ${data.count} IDs]\nFormat  : ${data.format}\nFirst   : ${data.ids[0]}\nLast    : ${data.ids[data.ids.length - 1]}\n\nSample (Top 10):\n` + data.ids.slice(0, 10).join('\n');
      decodeID(data.ids[0]);
    }
  } catch (err) {
    outputConsole.textContent = 'Network or server error: ' + err.message;
  }
}

async function decodeID(idStr) {
  const breakdownEl = document.getElementById('bit-breakdown-box');
  if (!breakdownEl) return;

  try {
    const res = await fetch(`/api/v1/ids/decode?id=${encodeURIComponent(idStr)}`);
    if (!res.ok) return;
    const data = await res.json();

    breakdownEl.innerHTML = `
      <h4>ID Structure Decode (${escapeHTML(data.format)})</h4>
      <div class="bit-field"><span>Raw ID:</span> <strong>${escapeHTML(data.raw)}</strong></div>
      <div class="bit-field"><span>Timestamp (UTC):</span> <span>${escapeHTML(data.timestamp)}</span></div>
      <div class="bit-field"><span>Millis Since Epoch:</span> <span>${data.millisSinceEpoch || 0} ms</span></div>
      <div class="bit-field"><span>Machine ID:</span> <span>${data.machineID || 0}</span></div>
      <div class="bit-field"><span>Sequence:</span> <span>${data.sequence || 0}</span></div>
      ${data.bitDetails ? `<div class="bit-field"><span>Bit Details:</span> <span style="font-family:var(--font-mono); font-size:0.75rem;">${escapeHTML(data.bitDetails)}</span></div>` : ''}
    `;
  } catch (err) {
    console.error('Decode failed:', err);
  }
}

// --- Cluster Operations ---
async function triggerRebalance() {
  const btn = document.getElementById('btn-rebalance');
  btn.disabled = true;
  btn.textContent = 'Rebalancing Pods...';

  try {
    const res = await fetch('/api/v1/admin/rebalance', { method: 'POST' });
    const data = await res.json();
    alert('Cluster rebalancing initiated: ' + (data.message || 'Success'));
    fetchMetrics();
  } catch (err) {
    alert('Rebalance failed: ' + err.message);
  } finally {
    btn.disabled = false;
    btn.textContent = '⚡ Rebalance Pod Machine IDs';
  }
}

// --- Developer API Keys ---
async function createAPIKey() {
  const tenantId = document.getElementById('key-tenant').value.trim() || 'tenant_dev';
  const tier = document.getElementById('key-tier').value;
  const resultBox = document.getElementById('key-result-box');

  try {
    const res = await fetch('/api/v1/auth/keys', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tenantId, tier })
    });
    const data = await res.json();

    if (res.ok) {
      resultBox.style.display = 'block';
      resultBox.innerHTML = `
        <div style="color: var(--accent-emerald); font-weight:700;">API Key Created Successfully!</div>
        <div style="margin-top:0.5rem; font-family:var(--font-mono);">Key: <span style="color:#f59e0b;">${escapeHTML(data.apiKey)}</span></div>
        <div style="font-size:0.8rem; color:var(--text-secondary); margin-top:0.25rem;">Tenant: ${escapeHTML(data.tenantId)} | Tier: ${escapeHTML(data.tier)} | Limit: ${data.rateLimit} req/s</div>
      `;
      updateCodeSnippet(data.apiKey);
    }
  } catch (err) {
    alert('Key creation failed: ' + err.message);
  }
}

function updateCodeSnippet(apiKeyOverride) {
  const lang = document.getElementById('snippet-lang').value;
  const codeBox = document.getElementById('code-snippet-box');
  const apiKey = apiKeyOverride || 'sk_live_enterprise_demo_key';

  let code = '';
  if (lang === 'curl') {
    code = `curl -X GET "http://localhost:8080/api/v1/ids/generate?format=snowflake&count=10" \\
  -H "X-API-Key: ${apiKey}"`;
  } else if (lang === 'go') {
    code = `package main

import (
  "fmt"
  "net/http"
  "io"
)

func main() {
  req, _ := http.NewRequest("GET", "http://localhost:8080/api/v1/ids/generate?format=snowflake", nil)
  req.Header.Set("X-API-Key", "${apiKey}")
  resp, err := http.DefaultClient.Do(req)
  if err == nil {
    body, _ := io.ReadAll(resp.Body)
    fmt.Println(string(body))
  }
}`;
  } else if (lang === 'js') {
    code = `const resp = await fetch('http://localhost:8080/api/v1/ids/generate?format=snowflake', {
  headers: { 'X-API-Key': '${apiKey}' }
});
const data = await resp.json();
console.log(data.id);`;
  } else if (lang === 'python') {
    code = `import requests

headers = {'X-API-Key': '${apiKey}'}
r = requests.get('http://localhost:8080/api/v1/ids/generate?format=snowflake', headers=headers)
print(r.json())`;
  }

  codeBox.textContent = code;
}

// --- Security & Blocklist ---
async function blockIP() {
  const ip = document.getElementById('block-ip-input').value.trim();
  const reason = document.getElementById('block-reason-input').value.trim() || 'Manual Security Enforcement';

  if (!ip) {
    alert('Please enter an IP address');
    return;
  }

  try {
    const res = await fetch('/api/v1/admin/security/block', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ip, action: 'block', reason })
    });
    const data = await res.json();
    alert(data.message || 'IP Blocked');
    fetchAuditLogs();
    fetchBlocklist();
  } catch (err) {
    alert('Failed to block IP: ' + err.message);
  }
}

async function fetchAuditLogs() {
  try {
    const res = await fetch('/api/v1/admin/security/audit');
    if (!res.ok) return;
    const data = await res.json();
    const tbody = document.getElementById('audit-log-tbody');
    if (!tbody) return;

    tbody.innerHTML = (data.logs || []).map(log => `
      <tr>
        <td>${escapeHTML(log.timestamp)}</td>
        <td><span class="badge badge-rebalancing">${escapeHTML(log.eventType)}</span></td>
        <td>${escapeHTML(log.clientIp || 'internal')}</td>
        <td>${escapeHTML(log.detail)}</td>
      </tr>
    `).join('');
  } catch (err) {
    console.error('Audit fetch error:', err);
  }
}

async function fetchBlocklist() {
  try {
    const res = await fetch('/api/v1/admin/security/blocklist');
    if (!res.ok) return;
    const data = await res.json();
    const tbody = document.getElementById('blocklist-tbody');
    if (!tbody) return;

    const entries = Object.entries(data.blockedIPs || {});
    if (entries.length === 0) {
      tbody.innerHTML = '<tr><td colspan="3" style="text-align:center; color:var(--text-muted);">No IP addresses currently blocked.</td></tr>';
      return;
    }

    tbody.innerHTML = entries.map(([ip, exp]) => `
      <tr>
        <td><strong>${escapeHTML(ip)}</strong></td>
        <td>${escapeHTML(exp)}</td>
        <td>
          <button class="btn btn-danger" style="padding:0.25rem 0.5rem; font-size:0.75rem;" onclick="unblockIP('${escapeHTML(ip)}')">Unblock</button>
        </td>
      </tr>
    `).join('');
  } catch (err) {
    console.error('Blocklist fetch error:', err);
  }
}

async function unblockIP(ip) {
  try {
    const res = await fetch('/api/v1/admin/security/block', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ip, action: 'unblock' })
    });
    const data = await res.json();
    alert(data.message || 'IP Unblocked');
    fetchBlocklist();
  } catch (err) {
    alert('Failed to unblock: ' + err.message);
  }
}

function formatNumber(num) {
  return new Intl.NumberFormat().format(num);
}

function escapeHTML(str) {
  if (!str) return '';
  return String(str).replace(/[&<>"']/g, m => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  })[m]);
}
