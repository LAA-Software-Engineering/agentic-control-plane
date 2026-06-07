(function () {
  const statusEl = document.getElementById('status');
  const runsList = document.getElementById('runs-list');
  const runDetail = document.getElementById('run-detail');
  const filterWorkflow = document.getElementById('filter-workflow');

  function setStatus(msg, isErr) {
    statusEl.textContent = msg;
    statusEl.className = isErr ? 'status-err' : '';
  }

  async function api(path) {
    const res = await fetch(path);
    const body = await res.json();
    if (!res.ok) {
      throw new Error(body.error || res.statusText);
    }
    return body;
  }

  function esc(s) {
    const d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
  }

  document.querySelectorAll('.tabs button').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.tabs button').forEach((b) => b.classList.remove('active'));
      document.querySelectorAll('.panel').forEach((p) => p.classList.remove('active'));
      btn.classList.add('active');
      document.getElementById('panel-' + btn.dataset.tab).classList.add('active');
      if (btn.dataset.tab === 'state') loadState();
    });
  });

  async function loadRuns() {
    setStatus('Loading runs…');
    runsList.innerHTML = '';
    runDetail.classList.add('hidden');
    runsList.classList.remove('hidden');
    const wf = filterWorkflow.value.trim();
    const q = wf ? '?workflow=' + encodeURIComponent(wf) : '';
    try {
      const data = await api('/api/runs' + q);
      setStatus('State: ' + data.statePath);
      if (!data.runs || data.runs.length === 0) {
        runsList.innerHTML = '<p class="meta">No runs found.</p>';
        return;
      }
      data.runs.forEach((r) => {
        const card = document.createElement('div');
        card.className = 'card';
        card.innerHTML =
          '<strong>' + esc(r.runId) + '</strong><br>' +
          '<span class="meta">' + esc(r.workflow) + ' · ' + esc(r.status) + ' · ' + esc(r.startedAt) + '</span>';
        card.addEventListener('click', () => showRun(r.runId));
        runsList.appendChild(card);
      });
    } catch (e) {
      setStatus(e.message, true);
    }
  }

  async function showRun(runId) {
    setStatus('Loading run ' + runId + '…');
    try {
      const data = await api('/api/runs/' + encodeURIComponent(runId));
      runsList.classList.add('hidden');
      runDetail.classList.remove('hidden');
      document.getElementById('run-title').textContent = runId;
      const run = data.run;
      document.getElementById('run-meta').textContent =
        run.workflow + ' · ' + run.status + ' · env ' + run.env;
      const linkEl = document.getElementById('run-trace-link');
      if (data.traceLink) {
        linkEl.classList.remove('hidden');
        linkEl.innerHTML = 'Trace: <a href="' + esc(data.traceLink) + '" target="_blank" rel="noopener">' + esc(data.traceLink) + '</a>';
      } else {
        linkEl.classList.add('hidden');
      }
      const stepsEl = document.getElementById('run-steps');
      if (!data.steps || data.steps.length === 0) {
        stepsEl.innerHTML = '<p class="meta">No steps recorded.</p>';
      } else {
        let html = '<table><thead><tr><th>Step</th><th>Status</th><th>Cost</th></tr></thead><tbody>';
        data.steps.forEach((s) => {
          html += '<tr><td>' + esc(s.stepId) + '</td><td>' + esc(s.status) + '</td><td>' + s.costUsd + '</td></tr>';
        });
        stepsEl.innerHTML = html + '</tbody></table>';
      }
      const evEl = document.getElementById('run-events');
      evEl.innerHTML = '';
      (data.events || []).forEach((e) => {
        const div = document.createElement('div');
        div.className = 'event event-' + (e.timelineGroup || 'other');
        const icon = e.timelineIcon ? e.timelineIcon + ' ' : '';
        div.innerHTML =
          '<span class="type">' + icon + esc(e.type) + '</span> ' +
          '<span class="meta">seq ' + e.seq +
          (e.actorType ? ' · ' + esc(e.actorType) : '') +
          (e.stepId ? ' · ' + esc(e.stepId) : '') + '</span>' +
          '<pre>' + esc(JSON.stringify(e.data, null, 2)) + '</pre>';
        evEl.appendChild(div);
      });
      const cpEl = document.getElementById('run-checkpoints');
      try {
        const cps = await api('/api/checkpoints?run=' + encodeURIComponent(runId));
        cpEl.innerHTML = '';
        if (!cps.checkpoints || cps.checkpoints.length === 0) {
          cpEl.innerHTML = '<p class="meta">No checkpoints.</p>';
        } else {
          cps.checkpoints.forEach((c) => {
            const card = document.createElement('div');
            card.className = 'card';
            card.style.cursor = 'default';
            card.innerHTML =
              '<strong>seq ' + c.seq + '</strong> ' + esc(c.stepId) + ' · ' + esc(c.status) +
              '<pre>' + esc(JSON.stringify(c.context, null, 2)) + '</pre>';
            cpEl.appendChild(card);
          });
        }
      } catch (_) {
        cpEl.innerHTML = '<p class="meta">Checkpoints unavailable.</p>';
      }
      setStatus('State: ' + data.statePath);
    } catch (e) {
      setStatus(e.message, true);
    }
  }

  async function loadState() {
    setStatus('Loading deployment state…');
    const el = document.getElementById('state-list');
    el.innerHTML = '';
    try {
      const data = await api('/api/state');
      setStatus('Environment: ' + data.environment + ' · ' + data.statePath);
      if (!data.resources || data.resources.length === 0) {
        el.innerHTML = '<p class="meta">No applied resources.</p>';
        return;
      }
      data.resources.forEach((r) => {
        const card = document.createElement('div');
        card.className = 'card';
        card.style.cursor = 'default';
        card.innerHTML =
          '<strong>' + esc(r.kind) + '/' + esc(r.name) + '</strong><br>' +
          '<span class="meta">hash ' + esc(r.specHash) + ' · ' + esc(r.appliedAt) + '</span>' +
          '<pre>' + esc(r.normalizedSpecJson.slice(0, 400)) + (r.normalizedSpecJson.length > 400 ? '…' : '') + '</pre>';
        el.appendChild(card);
      });
    } catch (e) {
      setStatus(e.message, true);
    }
  }

  document.getElementById('btn-refresh-runs').addEventListener('click', loadRuns);
  document.getElementById('btn-refresh-state').addEventListener('click', loadState);
  document.getElementById('btn-back-runs').addEventListener('click', () => {
    runDetail.classList.add('hidden');
    runsList.classList.remove('hidden');
    loadRuns();
  });

  loadRuns();
})();
