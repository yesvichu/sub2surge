(() => {
  "use strict";

  const $ = (id) => document.getElementById(id);
  const els = {
    form: $("form"),
    url: $("url"),
    urlLabel: $("url-label"),
    merge: $("merge"),
    mergeFields: $("merge-fields"),
    historyMenu: $("history-menu"),
    historyList: $("history-list"),
    historyCount: $("history-count"),
    historyClear: $("history-clear"),
    label1: $("label1"),
    label2: $("label2"),
    url2: $("url2"),
    ua: $("ua"),
    prefix: $("prefix"),
    filter: $("filter"),
    exclude: $("exclude"),
    sort: $("sort"),
    udp: $("udp"),
    tfo: $("tfo"),
    insecure: $("insecure"),
    rules: $("rules"),
    groupsMenu: $("groups-menu"),
    groupList: $("group-list"),
    groupCount: $("group-count"),
    groupsAll: $("groups-all"),
    groupsNone: $("groups-none"),
    convert: $("convert"),
    save: $("save"),
    surgeApply: $("surge-apply"),
    surgeTarget: $("surge-target"),
    surgeHint: $("surge-hint"),
    copy: $("copy"),
    wrap: $("wrap"),
    status: $("status"),
    summary: $("summary"),
    preview: $("preview"),
    filename: $("filename"),
    counts: $("counts"),
    version: $("version"),
    tabs: Array.from(document.querySelectorAll(".segment")),
  };

  const state = {
    config: "",
    list: "",
    mode: "conf",
    converted: 0,
    busy: false,
    groupDefs: [],
    selectedGroups: new Set(),
    groupsLoaded: false,
    groupSpec: "all",
    history: [],
    pendingGroups: null,
    surge: null,
  };

  const runtime = window.mygo;

  function call(method, ...args) {
    if (!runtime || typeof runtime.call !== "function") {
      return Promise.reject(
        new Error("请通过 MyGo 桌面应用运行：浏览器里无法调用转换器。")
      );
    }
    return runtime.call(method, ...args);
  }

  function currentText() {
    return state.mode === "conf" ? state.config : state.list;
  }

  function currentFilename() {
    const base = els.merge.checked ? "Surge-merged" : "Surge";
    return state.mode === "conf" ? `${base}.conf` : `${base.toLowerCase()}-nodes.txt`;
  }

  function lineCount(text) {
    if (!text) return 0;
    return text.replace(/\n$/, "").split("\n").length;
  }

  function setStatus(text, kind) {
    if (!text) {
      els.status.textContent = "";
      delete els.status.dataset.kind;
      return;
    }
    els.status.textContent = text;
    els.status.dataset.kind = kind || "busy";
  }

  function setBusy(busy) {
    state.busy = busy;
    els.convert.disabled = busy;
    els.convert.classList.toggle("is-busy", busy);
    const label = els.convert.querySelector(".button-label");
    if (label) label.textContent = busy ? "转换中…" : "转换";
    syncButtons();
  }

  function syncButtons() {
    const hasText = Boolean(currentText());
    els.save.disabled = state.busy || !hasText;
    els.copy.disabled = state.busy || !hasText;
    els.surgeApply.disabled =
      state.busy || !state.config || !state.surge || !state.surge.installed;
  }

  function describeSurge(surge) {
    if (!surge || !surge.installed) {
      els.surgeTarget.textContent = "不可用";
      els.surgeHint.textContent =
        (surge && surge.problem) || "没有检测到 Surge 或它的命令行工具。";
      return;
    }
    els.surgeTarget.textContent = `当前配置：${surge.current_name || "未知"}`;
    els.surgeHint.textContent = surge.managed
      ? "写入这份配置并让 Surge 热重载；原配置会自动备份。"
      : `「${surge.current_name}」不是 Sub2Surge 生成的，写入前会再确认一次。`;
  }

  function loadSurgeStatus() {
    return call("Converter.SurgeStatus")
      .then((surge) => {
        state.surge = surge || null;
        describeSurge(state.surge);
        syncButtons();
      })
      .catch((err) => {
        state.surge = null;
        els.surgeTarget.textContent = "不可用";
        els.surgeHint.textContent = (err && err.message) || String(err);
        syncButtons();
      });
  }

  async function applyToSurge() {
    if (state.busy) return;
    if (!state.config) {
      setStatus("请先转换，再写入 Surge。", "error");
      return;
    }

    let force = false;
    if (state.surge && state.surge.installed && state.surge.managed === false) {
      const name = state.surge.current_name || "当前配置";
      const ok = window.confirm(
        `「${name}」不是 Sub2Surge 生成的，继续会整份覆盖它（原文件会备份）。确定吗？`
      );
      if (!ok) return;
      force = true;
    }

    setBusy(true);
    setStatus("正在写入 Surge 并重载…", "busy");
    try {
      const res = await call("Converter.ApplyToSurge", {
        config: state.config,
        force,
      });
      const backup = res && res.backup_path ? `，原配置已备份` : "";
      setStatus(`已写入「${res.profile_name}」并让 Surge 重载生效${backup}。`, "ok");
      loadSurgeStatus();
    } catch (err) {
      setStatus((err && err.message) || String(err), "error");
    } finally {
      setBusy(false);
    }
  }

  function updateGroupCount() {
    if (!state.groupsLoaded) {
      els.groupCount.textContent = "全部";
      return;
    }
    const total = state.groupDefs.length;
    const selected = state.selectedGroups.size;
    els.groupCount.textContent =
      selected === total ? "全部" : `${selected}/${total}`;
  }

  function updateGroupSpec() {
    if (
      !state.groupsLoaded ||
      state.selectedGroups.size === state.groupDefs.length
    ) {
      state.groupSpec = "all";
    } else if (state.selectedGroups.size === 0) {
      state.groupSpec = "none";
    } else {
      state.groupSpec = Array.from(state.selectedGroups).join(",");
    }
    updateGroupCount();
  }

  function syncGroupsDisabled() {
    const disabled = !els.rules.checked;
    els.groupsMenu.classList.toggle("is-disabled", disabled);
    if (disabled) els.groupsMenu.open = false;
    for (const input of els.groupList.querySelectorAll("input")) {
      input.disabled = disabled;
    }
    els.groupsAll.disabled = disabled;
    els.groupsNone.disabled = disabled;
  }

  function renderGroups() {
    els.groupList.textContent = "";
    for (const def of state.groupDefs) {
      const row = document.createElement("label");
      row.className = "group-row";

      const input = document.createElement("input");
      input.type = "checkbox";
      input.checked = state.selectedGroups.has(def.id);
      input.addEventListener("change", () => {
        if (input.checked) state.selectedGroups.add(def.id);
        else state.selectedGroups.delete(def.id);
        updateGroupSpec();
      });

      const copy = document.createElement("span");
      copy.className = "group-copy";
      const name = document.createElement("span");
      name.className = "group-name";
      name.textContent = def.label || def.id;
      const desc = document.createElement("span");
      desc.className = "group-desc";
      desc.textContent = def.description || "";
      copy.appendChild(name);
      if (def.description) copy.appendChild(desc);

      row.appendChild(input);
      row.appendChild(copy);
      els.groupList.appendChild(row);
    }
    updateGroupCount();
    syncGroupsDisabled();
  }

  function syncMergeFields(focus) {
    const enabled = els.merge.checked;
    els.mergeFields.hidden = !enabled;
    els.urlLabel.textContent = enabled ? "订阅 A 链接" : "订阅链接";
    if (enabled && focus) els.url2.focus();
  }

  function loadRuleGroups() {
    call("Converter.RuleGroups")
      .then((defs) => {
        if (!Array.isArray(defs) || defs.length === 0) return;
        state.groupDefs = defs;
        state.groupsLoaded = true;
        state.selectedGroups = new Set(
          defs.filter((def) => def.default).map((def) => def.id)
        );
        if (state.pendingGroups !== null) {
          const wanted = state.pendingGroups;
          state.pendingGroups = null;
          if (wanted === "all") {
            state.selectedGroups = new Set(defs.map((def) => def.id));
          } else if (wanted === "none") {
            state.selectedGroups = new Set();
          } else {
            state.selectedGroups = new Set(wanted);
          }
        }
        updateGroupSpec();
        renderGroups();
      })
      .catch(() => {
        // Keep "all" as the safe fallback if the menu cannot load.
        els.groupCount.textContent = "全部";
      });
  }

  function resetPreviewScroll() {
    els.preview.scrollTop = 0;
    els.preview.scrollLeft = 0;
    // WebKit may restore/anchoring the scroll position after layout.
    requestAnimationFrame(() => {
      els.preview.scrollTop = 0;
      els.preview.scrollLeft = 0;
    });
  }

  function shortURL(raw) {
    const value = String(raw || "");
    try {
      const parsed = new URL(value);
      return parsed.hostname + (parsed.pathname === "/" ? "" : parsed.pathname);
    } catch {
      return value.length > 32 ? value.slice(0, 32) + "…" : value;
    }
  }

  function renderHistory() {
    els.historyCount.textContent = String(state.history.length);
    els.historyList.textContent = "";
    els.historyClear.disabled = state.history.length === 0;
    if (state.history.length === 0) {
      const empty = document.createElement("div");
      empty.className = "history-empty";
      empty.textContent = "还没有历史记录，转换成功后会自动保存。";
      els.historyList.appendChild(empty);
      return;
    }
    for (const entry of state.history) {
      const row = document.createElement("div");
      row.className = "history-row";

      const apply = document.createElement("button");
      apply.type = "button";
      apply.className = "history-apply";
      apply.addEventListener("click", () => applyHistory(entry));

      const title = document.createElement("span");
      title.className = "history-title";
      title.textContent = entry.name || shortURL(entry.url);
      const meta = document.createElement("span");
      meta.className = "history-meta";
      const urls = entry.merge
        ? `${shortURL(entry.url)} + ${shortURL(entry.url2)}`
        : shortURL(entry.url);
      const when = entry.updated_at
        ? new Date(entry.updated_at).toLocaleString()
        : "";
      meta.textContent = [entry.merge ? "合并" : "单个", `${entry.nodes || 0} 个节点`, when, urls]
        .filter(Boolean)
        .join(" · ");
      apply.appendChild(title);
      apply.appendChild(meta);

      const forget = document.createElement("button");
      forget.type = "button";
      forget.className = "history-forget";
      forget.title = "从历史中移除";
      forget.setAttribute("aria-label", `移除 ${title.textContent} 的历史记录`);
      forget.textContent = "×";
      forget.addEventListener("click", () => removeHistory(entry));

      row.appendChild(apply);
      row.appendChild(forget);
      els.historyList.appendChild(row);
    }
  }

  function loadHistory() {
    return call("Converter.History")
      .then((entries) => {
        state.history = Array.isArray(entries) ? entries : [];
        renderHistory();
      })
      .catch(() => {});
  }

  function applyHistory(entry) {
    els.url.value = entry.url || "";
    els.url2.value = entry.url2 || "";
    els.label1.value = entry.label1 || "订阅 A";
    els.label2.value = entry.label2 || "订阅 B";
    els.merge.checked = Boolean(entry.merge);
    els.ua.value = entry.ua || "";
    els.prefix.value = entry.prefix || "";
    els.filter.value = entry.filter || "";
    els.exclude.value = entry.exclude || "";
    els.sort.checked = Boolean(entry.sort);
    els.udp.checked = Boolean(entry.udp);
    els.tfo.checked = Boolean(entry.tfo);
    els.insecure.checked = Boolean(entry.insecure);
    els.rules.checked = entry.rules !== false;
    syncMergeFields(false);
    syncGroupsDisabled();

    const wanted = entry.groups === "all" || entry.groups === "none"
      ? entry.groups
      : String(entry.groups || "").split(",").filter(Boolean);
    if (state.groupsLoaded) {
      if (wanted === "all") {
        state.selectedGroups = new Set(state.groupDefs.map((def) => def.id));
      } else if (wanted === "none") {
        state.selectedGroups = new Set();
      } else {
        state.selectedGroups = new Set(wanted);
      }
      updateGroupSpec();
      renderGroups();
    } else {
      // Rule groups are still loading; restore the selection when they arrive.
      state.pendingGroups = wanted;
    }

    els.historyMenu.open = false;
    setStatus("已填入历史记录，点「转换」即可更新订阅。", "ok");
    els.url.focus();
  }

  async function removeHistory(entry) {
    try {
      state.history = (await call("Converter.ForgetHistory", entry.id)) || [];
      renderHistory();
    } catch (err) {
      setStatus((err && err.message) || String(err), "error");
    }
  }

  async function clearHistory() {
    if (!state.history.length) return;
    if (!window.confirm("确定清空所有订阅历史记录吗？")) return;
    try {
      state.history = (await call("Converter.ClearHistory")) || [];
      renderHistory();
      setStatus("订阅历史已清空。", "ok");
    } catch (err) {
      setStatus((err && err.message) || String(err), "error");
    }
  }

  function renderPreview() {
    const text = currentText();
    if (!text) {
      els.preview.innerHTML =
        '<span class="preview-empty">转换结果会显示在这里。</span>';
      els.counts.textContent = "";
      els.filename.textContent = currentFilename();
      syncButtons();
      return;
    }
    els.preview.textContent = text;
    resetPreviewScroll();
    els.filename.textContent = currentFilename();
    els.counts.textContent = `${lineCount(text)} 行`;
    syncButtons();
  }

  function setMode(mode) {
    state.mode = mode === "list" ? "list" : "conf";
    for (const tab of els.tabs) {
      const active = tab.dataset.mode === state.mode;
      tab.classList.toggle("active", active);
      tab.setAttribute("aria-selected", active ? "true" : "false");
    }
    renderPreview();
  }

  function metric(label, value, warn) {
    const wrap = document.createElement("span");
    wrap.className = "metric" + (warn ? " warn" : "");
    const strong = document.createElement("strong");
    strong.textContent = String(value);
    const text = document.createElement("span");
    text.textContent = label;
    wrap.appendChild(strong);
    wrap.appendChild(text);
    return wrap;
  }

  function renderSummary(res) {
    els.summary.hidden = false;
    els.summary.textContent = "";

    const metrics = document.createElement("div");
    metrics.className = "metrics";
    metrics.appendChild(metric("个解析", res.total));
    metrics.appendChild(metric("个可用", res.converted));
    if (res.sources > 1) {
      metrics.appendChild(metric("个来源", res.sources));
    }
    metrics.appendChild(metric("个跳过", res.skipped.length, res.skipped.length > 0));
    if (res.elapsed_ms >= 0) {
      metrics.appendChild(metric("ms", Math.max(0, res.elapsed_ms)));
    }
    els.summary.appendChild(metrics);

    if (res.warnings && res.warnings.length > 0) {
      for (const warning of res.warnings) {
        const line = document.createElement("div");
        line.className = "warning";
        line.textContent = warning;
        els.summary.appendChild(line);
      }
    }

    if (!res.skipped || res.skipped.length === 0) return;

    const details = document.createElement("details");
    details.className = "skip-details";
    const summary = document.createElement("summary");
    summary.textContent = `查看跳过的 ${res.skipped.length} 个节点`;
    details.appendChild(summary);

    const list = document.createElement("ul");
    list.className = "skip-list";
    const shown = res.skipped.slice(0, 40);
    for (const skip of shown) {
      const item = document.createElement("li");
      item.className = "skip-item";

      const name = document.createElement("span");
      name.className = "skip-name";
      name.textContent = skip.name || "（未命名节点）";
      if (skip.type) {
        const type = document.createElement("span");
        type.className = "skip-type";
        type.textContent = `  ${skip.type}`;
        name.appendChild(type);
      }

      const reason = document.createElement("span");
      reason.className = "skip-reason";
      reason.textContent = skip.reason || "无法转换";

      item.appendChild(name);
      item.appendChild(reason);
      list.appendChild(item);
    }
    details.appendChild(list);

    if (res.skipped.length > shown.length) {
      const more = document.createElement("div");
      more.className = "warning";
      more.textContent = `还有 ${res.skipped.length - shown.length} 个未显示`;
      details.appendChild(more);
    }

    els.summary.appendChild(details);
  }

  function requestBody() {
    return {
      url: els.url.value.trim(),
      url2: els.url2.value.trim(),
      label1: els.label1.value.trim() || "订阅 A",
      label2: els.label2.value.trim() || "订阅 B",
      merge: els.merge.checked,
      ua: els.ua.value.trim(),
      prefix: els.prefix.value,
      filter: els.filter.value,
      exclude: els.exclude.value,
      sort: els.sort.checked,
      udp: els.udp.checked,
      tfo: els.tfo.checked,
      insecure: els.insecure.checked,
      rules: els.rules.checked,
      groups: state.groupSpec,
    };
  }

  async function convert() {
    if (state.busy) return;
    const url = els.url.value.trim();
    if (!url) {
      setStatus("先填入 V2Ray 订阅链接。", "error");
      els.url.focus();
      return;
    }
    if (els.merge.checked) {
      if (!els.url2.value.trim()) {
        setStatus("合并模式需要第二个订阅链接。", "error");
        els.url2.focus();
        return;
      }
      const label1 = els.label1.value.trim() || "订阅 A";
      const label2 = els.label2.value.trim() || "订阅 B";
      if (label1 === label2) {
        setStatus("两个来源的标签不能相同。", "error");
        els.label2.focus();
        return;
      }
    }

    setBusy(true);
    setStatus("正在拉取订阅并转换…", "busy");
    els.summary.hidden = true;
    els.summary.textContent = "";

    try {
      const res = await call("Converter.Convert", requestBody());
      if (!res || !res.config) {
        throw new Error("转换器没有返回内容。");
      }
      state.config = res.config;
      state.list = res.list || "";
      state.converted = res.converted || 0;
      renderSummary(res);
      renderPreview();
      loadHistory();
      const skipped = res.skipped ? res.skipped.length : 0;
      const sourceText = res.sources > 1 ? `合并 ${res.sources} 个订阅：` : "";
      setStatus(
        skipped > 0
          ? `${sourceText}${res.converted} 个节点可用，${skipped} 个跳过。`
          : `${sourceText}${res.converted} 个节点可用。`,
        "ok"
      );
    } catch (err) {
      state.config = "";
      state.list = "";
      state.converted = 0;
      els.summary.hidden = true;
      els.summary.textContent = "";
      renderPreview();
      setStatus((err && err.message) || String(err), "error");
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    const content = currentText();
    if (!content || state.busy) return;
    try {
      const path = await call(
        "Converter.Save",
        currentFilename(),
        content
      );
      if (path) setStatus(`已保存到 ${path}`, "ok");
    } catch (err) {
      setStatus((err && err.message) || String(err), "error");
    }
  }

  async function copy() {
    const content = currentText();
    if (!content || state.busy) return;
    try {
      await navigator.clipboard.writeText(content);
      setStatus("已复制到剪贴板。", "ok");
    } catch (err) {
      // WebView clipboard access can be denied; fall back to a selection.
      try {
        const area = document.createElement("textarea");
        area.value = content;
        area.style.position = "fixed";
        area.style.opacity = "0";
        document.body.appendChild(area);
        area.select();
        const ok = document.execCommand("copy");
        document.body.removeChild(area);
        if (!ok) throw new Error("复制失败");
        setStatus("已复制到剪贴板。", "ok");
      } catch {
        setStatus("无法访问剪贴板，请手动选择预览内容复制。", "error");
      }
    }
  }

  els.form.addEventListener("submit", (event) => {
    event.preventDefault();
    convert();
  });

  for (const tab of els.tabs) {
    tab.addEventListener("click", () => setMode(tab.dataset.mode));
  }

  els.save.addEventListener("click", save);
  els.surgeApply.addEventListener("click", applyToSurge);
  els.historyClear.addEventListener("click", clearHistory);
  els.copy.addEventListener("click", copy);
  els.rules.addEventListener("change", syncGroupsDisabled);
  els.merge.addEventListener("change", () => {
    syncMergeFields(els.merge.checked);
    state.config = "";
    state.list = "";
    state.converted = 0;
    els.summary.hidden = true;
    els.summary.textContent = "";
    setStatus("", "");
    renderPreview();
  });
  els.groupsAll.addEventListener("click", () => {
    if (!state.groupsLoaded) return;
    state.selectedGroups = new Set(state.groupDefs.map((def) => def.id));
    updateGroupSpec();
    renderGroups();
  });
  els.groupsNone.addEventListener("click", () => {
    if (!state.groupsLoaded) return;
    state.selectedGroups = new Set();
    updateGroupSpec();
    renderGroups();
  });
  els.wrap.addEventListener("change", () => {
    els.preview.classList.toggle("wrap", els.wrap.checked);
  });

  document.addEventListener("keydown", (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault();
      convert();
    }
  });

  if (!runtime) {
    setStatus(
      "当前是浏览器预览。请在 MyGo 桌面应用中运行，才能转换并保存文件。",
      "error"
    );
  }

  call("Converter.Version")
    .then((v) => {
      if (v) els.version.textContent = `v${v}`;
    })
    .catch(() => {});

  syncMergeFields(false);
  loadRuleGroups();
  loadHistory();
  loadSurgeStatus();
  syncGroupsDisabled();
  renderPreview();
  els.url.focus();
})();
