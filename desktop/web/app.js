(() => {
  "use strict";

  const $ = (id) => document.getElementById(id);
  const els = {
    form: $("form"),
    url: $("url"),
    urlLabel: $("url-label"),
    merge: $("merge"),
    mergeFields: $("merge-fields"),
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
  syncGroupsDisabled();
  renderPreview();
  els.url.focus();
})();
