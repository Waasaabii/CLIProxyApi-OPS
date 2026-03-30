(() => {
  const AUTH_KEY = "cli-proxy-auth";
  const PANEL_ID = "cpa-ops-panel";
  const CAPTURED_KEY = "__cpaOpsManagementKey";
  const BUTTON_BOUND_ATTR = "data-cpa-ops-bound";
  const INTERNAL_HEADER = "X-CPA-OPS-Internal";

  installRequestCapture();

  function getManagementKey() {
    const captured = window[CAPTURED_KEY];
    if (captured) return captured;

    const direct = localStorage.getItem("managementKey");
    if (direct) return direct;

    const raw = localStorage.getItem(AUTH_KEY);
    if (!raw) return "";

    try {
      const parsed = JSON.parse(raw);
      if (typeof parsed === "string") return parsed;
      return (
        parsed.managementKey ||
        parsed.key ||
        parsed.token ||
        (parsed.auth && parsed.auth.managementKey) ||
        ""
      );
    } catch {
      return raw;
    }
  }

  function installRequestCapture() {
    const originalFetch = window.fetch;
    if (typeof originalFetch === "function") {
      window.fetch = async (...args) => {
        tryCaptureFromFetchArgs(args);
        return originalFetch.apply(window, args);
      };
    }

    const originalOpen = XMLHttpRequest.prototype.open;
    const originalSetRequestHeader = XMLHttpRequest.prototype.setRequestHeader;
    XMLHttpRequest.prototype.open = function(method, url, ...rest) {
      this.__cpaOpsUrl = url;
      return originalOpen.call(this, method, url, ...rest);
    };
    XMLHttpRequest.prototype.setRequestHeader = function(name, value) {
      tryCaptureHeader(this.__cpaOpsUrl, name, value);
      return originalSetRequestHeader.call(this, name, value);
    };
  }

  function tryCaptureFromFetchArgs(args) {
    const [input, init] = args;
    const url =
      typeof input === "string"
        ? input
        : input && typeof input.url === "string"
          ? input.url
          : "";
    if (init && init.headers) {
      tryCaptureHeaders(url, init.headers);
      return;
    }
    if (input && input.headers) {
      tryCaptureHeaders(url, input.headers);
    }
  }

  function tryCaptureHeaders(url, headersLike) {
    if (!headersLike) return;
    if (readHeaderValue(headersLike, INTERNAL_HEADER) === "1") return;
    if (headersLike instanceof Headers) {
      const value = headersLike.get("Authorization");
      tryCaptureHeader(url, "Authorization", value);
      return;
    }
    if (Array.isArray(headersLike)) {
      headersLike.forEach(([name, value]) => tryCaptureHeader(url, name, value));
      return;
    }
    Object.entries(headersLike).forEach(([name, value]) => tryCaptureHeader(url, name, value));
  }

  function tryCaptureHeader(url, name, value) {
    if (!url || !String(url).includes("/v0/management")) return;
    if (String(name).toLowerCase() !== "authorization") return;
    if (!value) return;
    const normalized = String(value).trim();
    if (!normalized) return;
    const token = normalized.replace(/^Bearer\s+/i, "");
    if (window[CAPTURED_KEY] === token) return;
    window[CAPTURED_KEY] = token;
    refreshAllPanels();
  }

  function readHeaderValue(headersLike, name) {
    if (!headersLike) return "";
    if (headersLike instanceof Headers) {
      return String(headersLike.get(name) || "").trim();
    }
    if (Array.isArray(headersLike)) {
      const pair = headersLike.find(([headerName]) => String(headerName).toLowerCase() === String(name).toLowerCase());
      return pair ? String(pair[1] || "").trim() : "";
    }
    const entry = Object.entries(headersLike).find(([headerName]) => String(headerName).toLowerCase() === String(name).toLowerCase());
    return entry ? String(entry[1] || "").trim() : "";
  }

  async function request(path, options = {}) {
    const managementKey = getManagementKey();
    const headers = new Headers(options.headers || {});
    if (managementKey) {
      headers.set("Authorization", `Bearer ${managementKey}`);
    }
    headers.set(INTERNAL_HEADER, "1");
    headers.set("Accept", "application/json");
    const response = await fetch(path, { ...options, headers });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || `${response.status}`);
    }
    return response.json();
  }

  async function requestResponse(path, options = {}) {
    const managementKey = getManagementKey();
    const headers = new Headers(options.headers || {});
    if (managementKey) {
      headers.set("Authorization", `Bearer ${managementKey}`);
    }
    headers.set(INTERNAL_HEADER, "1");
    headers.set("Accept", "application/json");
    const response = await fetch(path, { ...options, headers });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || `${response.status}`);
    }
    return response;
  }

  function textOf(node) {
    return (node && node.textContent ? node.textContent : "").trim();
  }

  function classNameOf(selector, fallback = "") {
    const node = document.querySelector(selector);
    return node && node.className ? node.className : fallback;
  }

  function findCheckUpdateButton() {
    return Array.from(document.querySelectorAll("button")).find((button) => textOf(button).includes("检查更新")) || null;
  }

  function getPreferredLocale() {
    const locale = String(navigator.language || navigator.userLanguage || "").trim();
    return locale || "zh-CN";
  }

  function isSystemPage() {
    return String(location.hash || "").includes("/system") || Boolean(findCheckUpdateButton());
  }

  function buildTheme() {
    const aboutCard = document.querySelector('[class*="SystemPage-module__aboutCard"]');
    const content = document.querySelector('[class*="SystemPage-module__content"]');
    const checkButton = findCheckUpdateButton();
   return {
      aboutCard,
      content,
      cardClass: "card",
      cardHeaderClass: classNameOf(".card-header", "card-header"),
      sectionDescriptionClass: classNameOf('[class*="SystemPage-module__sectionDescription"]', ""),
      aboutInfoGridClass: classNameOf('[class*="SystemPage-module__aboutInfoGrid"]', ""),
      infoTileClass: classNameOf('div[class*="SystemPage-module__infoTile"]', classNameOf('[class*="SystemPage-module__infoTile"]', "")),
      tileLabelClass: classNameOf('[class*="SystemPage-module__tileLabel"]', ""),
      tileValueClass: classNameOf('[class*="SystemPage-module__tileValue"]', ""),
      tileSubClass: classNameOf('[class*="SystemPage-module__tileSub"]', ""),
      secondaryButtonClass: classNameOf(".btn.btn-secondary", "btn btn-secondary btn-sm"),
      ghostButtonClass: checkButton && checkButton.className ? checkButton.className : classNameOf(".btn.btn-ghost", "btn btn-ghost btn-sm"),
    };
  }

  function createMetricTile(theme, label, key) {
    const tile = document.createElement("div");
    if (theme.infoTileClass) {
      tile.className = theme.infoTileClass;
    } else {
      tile.style.cssText = "padding:16px 24px;border:1px solid var(--border-color);border-radius:12px;background:var(--bg-secondary);";
    }

    const labelNode = document.createElement("div");
    if (theme.tileLabelClass) {
      labelNode.className = theme.tileLabelClass;
    } else {
      labelNode.style.cssText = "font-size:13px;color:var(--text-secondary);font-weight:600;";
    }
    labelNode.textContent = label;

    const valueNode = document.createElement("div");
    if (theme.tileValueClass) {
      valueNode.className = theme.tileValueClass;
    } else {
      valueNode.style.cssText = "margin-top:6px;font-size:22px;font-weight:700;color:var(--text-primary);line-height:1.25;";
    }
    valueNode.dataset.key = key;
    valueNode.textContent = "未知";

    tile.appendChild(labelNode);
    tile.appendChild(valueNode);
    return tile;
  }

  function createLabel(theme, text) {
    const label = document.createElement("div");
    if (theme.tileLabelClass) {
      label.className = theme.tileLabelClass;
    } else {
      label.style.cssText = "font-size:13px;color:var(--text-secondary);font-weight:600;";
    }
    label.textContent = text;
    return label;
  }

  function createPanel(theme) {
    const panel = document.createElement("section");
    panel.id = PANEL_ID;
    panel.className = theme.cardClass || "card";
    panel.style.marginTop = "16px";

    const header = document.createElement("div");
    header.className = theme.cardHeaderClass || "card-header";
    header.textContent = "运维更新";

    const description = document.createElement("p");
    if (theme.sectionDescriptionClass) {
      description.className = theme.sectionDescriptionClass;
    } else {
      description.style.cssText = "margin:0 24px 16px;color:var(--text-secondary);";
    }
    description.textContent = "点击原页面“检查更新”后加载，不改动 CPA 原有更新入口。";

    const grid = document.createElement("div");
    if (theme.aboutInfoGridClass) {
      grid.className = theme.aboutInfoGridClass;
    } else {
      grid.style.cssText = "display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:16px;padding:0 24px;";
    }
    grid.appendChild(createMetricTile(theme, "当前版本", "current"));
    grid.appendChild(createMetricTile(theme, "最新版本", "latest"));
    grid.appendChild(createMetricTile(theme, "发布时间", "published"));
    grid.appendChild(createMetricTile(theme, "容器状态", "container"));

    const actionRow = document.createElement("div");
    actionRow.style.cssText = "display:flex;align-items:center;gap:12px;flex-wrap:wrap;padding:16px 24px 0;";

    const updateButton = document.createElement("button");
    updateButton.type = "button";
    updateButton.className = theme.secondaryButtonClass || theme.ghostButtonClass || "btn btn-secondary btn-sm";
    updateButton.textContent = "立即更新并重启";
    updateButton.disabled = true;

    const status = document.createElement("span");
    status.className = theme.tileSubClass || "";
    status.dataset.role = "status";
    if (!status.className) {
      status.style.cssText = "font-size:13px;color:var(--text-secondary);";
    }
    status.textContent = "等待检查结果";

    actionRow.appendChild(updateButton);
    actionRow.appendChild(status);

    const adviceWrap = document.createElement("div");
    adviceWrap.style.cssText = "padding:16px 24px 0;";
    adviceWrap.appendChild(createLabel(theme, "更新建议"));

    const advice = document.createElement("div");
    advice.dataset.role = "advice";
    advice.style.cssText =
      "margin-top:8px;padding:12px 14px;border-radius:8px;border:1px solid var(--border-color);" +
      "background:var(--bg-secondary);color:var(--text-primary);white-space:pre-wrap;word-break:break-word;" +
      "font-size:13px;line-height:1.7;";
    advice.textContent = "等待检查更新后生成建议...";
    adviceWrap.appendChild(advice);

    const notesWrap = document.createElement("div");
    notesWrap.style.cssText = "padding:16px 24px 0;";
    notesWrap.appendChild(createLabel(theme, "Release 说明"));

    const notes = document.createElement("pre");
    notes.dataset.role = "notes";
    notes.style.cssText =
      "margin:8px 0 0;padding:12px 14px;border-radius:8px;border:1px solid var(--border-color);" +
      "background:var(--bg-secondary);color:var(--text-primary);white-space:pre-wrap;word-break:break-word;" +
      "font:12.5px/1.6 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,Liberation Mono,Courier New,monospace;" +
      "max-height:320px;overflow:auto;";
    notes.textContent = "等待检查更新后加载 release 说明...";
    notesWrap.appendChild(notes);

    const logWrap = document.createElement("div");
    logWrap.style.cssText = "display:none;padding:16px 24px 24px;";
    logWrap.dataset.role = "logWrap";
    logWrap.appendChild(createLabel(theme, "任务日志"));

    const log = document.createElement("pre");
    log.dataset.role = "log";
    log.style.cssText =
      "margin:8px 0 0;padding:12px 14px;border-radius:8px;border:1px solid var(--border-color);" +
      "background:var(--bg-secondary);color:var(--text-primary);white-space:pre-wrap;word-break:break-word;" +
      "font:12px/1.55 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,Liberation Mono,Courier New,monospace;" +
      "max-height:240px;overflow:auto;";
    logWrap.appendChild(log);

    panel.appendChild(header);
    panel.appendChild(description);
    panel.appendChild(grid);
    panel.appendChild(actionRow);
    panel.appendChild(adviceWrap);
    panel.appendChild(notesWrap);
    panel.appendChild(logWrap);
    return panel;
  }

  function ensurePanel() {
    if (!isSystemPage()) return null;
    let panel = document.getElementById(PANEL_ID);
    if (panel && document.contains(panel)) {
      return panel;
    }

    const theme = buildTheme();
    panel = createPanel(theme);
    if (theme.aboutCard && theme.aboutCard.parentElement) {
      theme.aboutCard.insertAdjacentElement("afterend", panel);
      return panel;
    }
    if (theme.content) {
      theme.content.prepend(panel);
      return panel;
    }
    return null;
  }

  function refreshAllPanels() {
    const panel = document.getElementById(PANEL_ID);
    if (panel) {
      refreshPanel(panel);
    }
  }

  function formatTimestamp(value) {
    if (!value) return "未知";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString("zh-CN", { hour12: false });
  }

  function withTimeout(promise, timeoutMs, message) {
    return new Promise((resolve, reject) => {
      const timer = window.setTimeout(() => reject(new Error(message)), timeoutMs);
      promise.then(
        (value) => {
          window.clearTimeout(timer);
          resolve(value);
        },
        (error) => {
          window.clearTimeout(timer);
          reject(error);
        }
      );
    });
  }

  function findNativeInfoValueNode(labelText) {
    const labels = Array.from(document.querySelectorAll("div, span, p, strong"))
      .filter((node) => textOf(node) === labelText);

    for (const label of labels) {
      const tile = label.closest('[class*="infoTile"]');
      if (tile) {
        const directValue = tile.querySelector('[class*="tileValue"]');
        if (directValue && directValue !== label) {
          return directValue;
        }

        const tileCandidates = Array.from(tile.querySelectorAll("div, span, p, strong"))
          .filter((candidate) => candidate !== label && textOf(candidate) && textOf(candidate) !== labelText);
        if (tileCandidates.length > 0) {
          return tileCandidates[tileCandidates.length - 1];
        }
      }

      const parent = label.parentElement;
      if (!parent) continue;

      const children = Array.from(parent.children);
      const labelIndex = children.indexOf(label);
      for (let index = children.length - 1; index > labelIndex; index -= 1) {
        const candidate = children[index];
        if (!candidate || candidate.tagName === "BUTTON") continue;
        if (!textOf(candidate)) continue;
        return candidate;
      }

      let sibling = label.nextElementSibling;
      while (sibling) {
        if (sibling.tagName !== "BUTTON" && textOf(sibling)) {
          return sibling;
        }
        sibling = sibling.nextElementSibling;
      }
    }

    return null;
  }

  function setNativeInfoValue(labelText, value) {
    const target = findNativeInfoValueNode(labelText);
    if (!target || !value) return false;
    target.textContent = value;
    return true;
  }

  async function fetchNativeManagementMeta() {
    try {
      const response = await requestResponse("/v0/management/config");
      return {
        currentVersion: response.headers.get("X-CPA-VERSION") || "",
        currentBuildDate: response.headers.get("X-CPA-BUILD-DATE") || "",
      };
    } catch {
      return null;
    }
  }

  function syncNativeOverview(version, nativeMeta) {
    const currentVersion =
      (nativeMeta && nativeMeta.currentVersion) ||
      (version && version.currentVersion) ||
      "";
    const currentBuildDate =
      (nativeMeta && nativeMeta.currentBuildDate) ||
      "";

    if (currentVersion) {
      setNativeInfoValue("CLI Proxy API 版本", currentVersion);
    }
    if (currentBuildDate) {
      setNativeInfoValue("构建时间", formatTimestamp(currentBuildDate));
    }
  }

  function setStatus(panel, text, tone) {
    const status = panel.querySelector('[data-role="status"]');
    if (!status) return;
    status.textContent = text;
    status.style.color = "var(--text-secondary)";
    status.style.fontWeight = "500";
    if (tone === "success") {
      status.style.color = "var(--success-badge-text, var(--text-primary))";
    } else if (tone === "warning") {
      status.style.color = "var(--warning-text, var(--warning-color))";
      status.style.fontWeight = "700";
    } else if (tone === "error") {
      status.style.color = "var(--error-color)";
      status.style.fontWeight = "700";
    }
  }

  async function refreshPanel(panel) {
    const notes = panel.querySelector('[data-role="notes"]');
    const advice = panel.querySelector('[data-role="advice"]');
    const updateButton = panel.querySelector("button");
    setStatus(panel, "正在检查运维版本信息...", "default");
    advice.textContent = "正在分析版本差异...";
    notes.textContent = "正在加载 release 说明...";
    updateButton.disabled = true;

    try {
      const [versionResult, releaseResult, statusResult, nativeMetaResult] = await Promise.allSettled([
        request("/ops/api/version"),
        withTimeout(
          request(`/ops/api/release-notes?locale=${encodeURIComponent(getPreferredLocale())}`),
          5000,
          "release 说明加载超时"
        ),
        request("/ops/api/status"),
        fetchNativeManagementMeta(),
      ]);
      const version = versionResult.status === "fulfilled" ? versionResult.value : null;
      const release = releaseResult.status === "fulfilled" ? releaseResult.value : null;
      const status = statusResult.status === "fulfilled" ? statusResult.value : null;
      const nativeMeta = nativeMetaResult.status === "fulfilled" ? nativeMetaResult.value : null;

      if (!version && !status) {
        const primaryError =
          versionResult.status === "rejected"
            ? versionResult.reason
            : statusResult.status === "rejected"
              ? statusResult.reason
              : new Error("运维信息不可用");
        throw primaryError;
      }

      panel.querySelector('[data-key="current"]').textContent = (version && version.currentVersion) || "未知";
      panel.querySelector('[data-key="latest"]').textContent = (version && version.latestVersion) || "未知";
      panel.querySelector('[data-key="published"]').textContent = formatTimestamp((release && release.publishedAt) || (version && version.publishedAt) || "");
      panel.querySelector('[data-key="container"]').textContent = (status && status.state) || "未知";
      syncNativeOverview(version, nativeMeta);
      advice.textContent =
        (release && release.updateRecommendation) ||
        (version && version.updateRecommendation) ||
        ((version && version.hasUpdate) ? "发现新版本，建议尽快安排更新。" : "当前已是最新版本。");
      notes.textContent =
        (release && release.releaseNotes) ||
        (version && version.releaseNotes) ||
        (releaseResult.status === "rejected" ? "release 说明暂时不可用，已显示缓存版本信息。" : "暂无 release 说明");

      if (version && version.hasUpdate) {
        const label = version.behindCount > 1 ? `发现 ${version.behindCount} 个待更新版本，可立即更新并重启` : "发现新版本，可立即更新并重启";
        setStatus(panel, label, "warning");
        updateButton.disabled = false;
      } else {
        setStatus(panel, "当前已是最新版本", "success");
        updateButton.disabled = true;
      }
    } catch (error) {
      setStatus(panel, `运维信息加载失败: ${error.message}`, "error");
      advice.textContent = "未能生成更新建议，请先确认主服务和管理密钥都可用。";
      notes.textContent = "请确认已通过管理密钥登录，并且 cpa-ops 已正常启动。";
    }
  }

  async function pollTask(panel, taskId) {
    const updateButton = panel.querySelector("button");
    const logWrap = panel.querySelector('[data-role="logWrap"]');
    const log = panel.querySelector('[data-role="log"]');
    logWrap.style.display = "block";

    while (true) {
      const task = await request(`/ops/api/tasks/${taskId}`);
      log.textContent = task.log || "";
      setStatus(panel, `任务状态: ${task.status}`, "default");

      if (task.status === "succeeded" || task.status === "failed") {
        if (task.error) {
          setStatus(panel, `任务失败: ${task.error}`, "error");
        }
        updateButton.disabled = true;
        await refreshPanel(panel);
        return;
      }

      await new Promise((resolve) => setTimeout(resolve, 2000));
    }
  }

  function wirePanel(panel) {
    const updateButton = panel.querySelector("button");
    if (updateButton.dataset.cpaOpsActionBound === "1") return;
    updateButton.dataset.cpaOpsActionBound = "1";

    updateButton.addEventListener("click", async () => {
      updateButton.disabled = true;
      setStatus(panel, "正在提交更新任务...", "default");
      try {
        const task = await request("/ops/api/update", { method: "POST" });
        await pollTask(panel, task.id);
      } catch (error) {
        setStatus(panel, `更新提交失败: ${error.message}`, "error");
      }
    });
  }

  function showPanelAfterCheckClick() {
    window.setTimeout(async () => {
      const panel = ensurePanel();
      if (!panel) return;
      wirePanel(panel);
      await refreshPanel(panel);
    }, 0);
  }

  function bindCheckUpdateButton() {
    if (!isSystemPage()) return;
    const button = findCheckUpdateButton();
    if (!button || button.getAttribute(BUTTON_BOUND_ATTR) === "1") {
      return;
    }
    button.setAttribute(BUTTON_BOUND_ATTR, "1");
    button.addEventListener("click", showPanelAfterCheckClick);
  }

  const observer = new MutationObserver(() => bindCheckUpdateButton());
  observer.observe(document.documentElement, { childList: true, subtree: true });
  window.addEventListener("popstate", bindCheckUpdateButton);
  window.addEventListener("hashchange", bindCheckUpdateButton);
  document.addEventListener("DOMContentLoaded", bindCheckUpdateButton);
  setInterval(bindCheckUpdateButton, 2000);
})();
