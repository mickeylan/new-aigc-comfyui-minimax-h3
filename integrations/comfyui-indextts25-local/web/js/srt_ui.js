const { app } = window.comfyAPI.app;

const NODE_TYPES = new Set([
    "IndexTTS25LocalSRTParse",
    "IndexTTS25LocalSRTSingleVoiceGenerate",
]);

const STYLE = `
.indextts-srt-panel{display:flex;flex-direction:column;gap:8px;padding:8px;box-sizing:border-box;color:var(--fg-color,#ddd);font:12px sans-serif;background:rgba(20,20,20,.45);border:1px solid #555;border-radius:7px}
.indextts-srt-actions{display:flex;gap:6px;flex-wrap:wrap}
.indextts-srt-panel button{border:1px solid #666;border-radius:5px;background:#303030;color:#eee;padding:6px 10px;cursor:pointer}
.indextts-srt-panel button:hover{background:#414141}
.indextts-srt-file{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.indextts-srt-summary{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:5px;color:#bbb}
.indextts-srt-summary span{background:#242424;border-radius:4px;padding:5px}
.indextts-srt-roles{color:#9fd3ff;white-space:normal;overflow-wrap:anywhere}
.indextts-srt-warning{color:#ffcb6b;white-space:normal}
`;

const EXAMPLE = `1\n00:00:00,000 --> 00:00:03,000\n[角色=上官若琳; 情绪=克制而欣喜; 语言=ZH]\n姐姐，终于等到你了。\n\n2\n00:00:03,500 --> 00:00:06,000\n[角色=舒寒; 表达=低声]\n我回来了。`;

function installStyle() {
    if (document.getElementById("indextts-srt-ui-style")) return;
    const style = document.createElement("style");
    style.id = "indextts-srt-ui-style";
    style.textContent = STYLE;
    document.head.appendChild(style);
}

function widget(node, name) {
    return node.widgets?.find((item) => item.name === name);
}

function setWidget(node, name, value) {
    const target = widget(node, name);
    if (!target) return;
    target.value = value;
    target.callback?.(value, app.canvas, node, app.canvas?.graph_mouse);
    node.graph?.setDirtyCanvas(true, true);
}

function timestampSeconds(line) {
    const match = line.match(/^(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})\s*-->\s*(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})/);
    if (!match) return null;
    const seconds = (offset) => Number(match[offset]) * 3600 + Number(match[offset + 1]) * 60 + Number(match[offset + 2]) + Number(match[offset + 3]) / (10 ** match[offset + 3].length);
    return [seconds(1), seconds(5)];
}

function inspectSRT(text) {
    const blocks = text.replace(/^\uFEFF/, "").replace(/\r\n?/g, "\n").trim().split(/\n\s*\n/).filter(Boolean);
    const roles = new Set();
    let first = Number.POSITIVE_INFINITY;
    let last = 0;
    let valid = 0;
    for (const block of blocks) {
        const lines = block.split("\n").map((line) => line.trim());
        const time = lines.map(timestampSeconds).find(Boolean);
        if (!time) continue;
        valid += 1;
        first = Math.min(first, time[0]);
        last = Math.max(last, time[1]);
        for (const line of lines) {
            const jsonRole = line.match(/["'](?:character|role|角色)["']\s*[:：]\s*["']([^"']+)/i);
            const pairRole = line.match(/(?:^|[\[{（(;,；，])\s*(?:character|role|角色)\s*[:：=]\s*([^;；,，\]}）)]+)/i);
            const role = (jsonRole?.[1] || pairRole?.[1] || "").trim();
            if (role) roles.add(role);
        }
    }
    return { count: valid, roles: [...roles].sort(), first: Number.isFinite(first) ? first : 0, last };
}

function formatTime(seconds) {
    const minutes = Math.floor(seconds / 60);
    const remain = (seconds - minutes * 60).toFixed(1).padStart(4, "0");
    return `${String(minutes).padStart(2, "0")}:${remain}`;
}

function createPanel(node) {
    installStyle();
    const root = document.createElement("div");
    root.className = "indextts-srt-panel";
    root.innerHTML = `
      <div class="indextts-srt-actions">
        <button type="button" data-action="open">选择 SRT 文件</button>
        <button type="button" data-action="example">填入示例</button>
        <button type="button" data-action="clear">清空</button>
      </div>
      <input type="file" accept=".srt,application/x-subrip,text/plain" hidden>
      <div class="indextts-srt-file">尚未选择文件，可粘贴 SRT 文本</div>
      <div class="indextts-srt-summary"><span>字幕 0 条</span><span>角色 0 个</span><span>范围 00:00.0</span></div>
      <div class="indextts-srt-roles">角色：未识别</div>
      <div class="indextts-srt-warning"></div>`;
    const picker = root.querySelector("input[type=file]");
    const fileLabel = root.querySelector(".indextts-srt-file");
    const summary = root.querySelector(".indextts-srt-summary");
    const roles = root.querySelector(".indextts-srt-roles");
    const warning = root.querySelector(".indextts-srt-warning");

    const update = (text, filename = "") => {
        const info = inspectSRT(text || "");
        fileLabel.textContent = filename || (text ? "已载入粘贴/示例内容" : "尚未选择文件，可粘贴 SRT 文本");
        summary.children[0].textContent = `字幕 ${info.count} 条`;
        summary.children[1].textContent = `角色 ${info.roles.length} 个`;
        summary.children[2].textContent = `范围 ${formatTime(info.first)}–${formatTime(info.last)}`;
        roles.textContent = `角色：${info.roles.join("、") || "未识别"}`;
        warning.textContent = info.roles.length > 1
            ? "多角色字幕：请为每个角色复制一个单角色生成节点，并连接各自参考音频。"
            : info.count === 0 && text ? "未识别到有效 SRT 时间码，请检查文件格式。" : "";
        if (info.roles.length === 1 && widget(node, "character") && !widget(node, "character").value) {
            setWidget(node, "character", info.roles[0]);
        }
    };

    root.querySelector('[data-action="open"]').onclick = () => picker.click();
    root.querySelector('[data-action="example"]').onclick = () => {
        setWidget(node, "srt_text", EXAMPLE);
        setWidget(node, "srt_path", "");
        update(EXAMPLE, "内置双角色示例（未上传）");
    };
    root.querySelector('[data-action="clear"]').onclick = () => {
        setWidget(node, "srt_text", "");
        setWidget(node, "srt_path", "");
        picker.value = "";
        update("");
    };
    picker.onchange = async () => {
        const file = picker.files?.[0];
        if (!file) return;
        if (!file.name.toLowerCase().endsWith(".srt")) {
            warning.textContent = "请选择 .srt 字幕文件。";
            return;
        }
        if (file.size > 2 * 1024 * 1024) {
            warning.textContent = "SRT 文件不能超过 2 MiB。";
            return;
        }
        const text = await file.text();
        setWidget(node, "srt_text", text.replace(/^\uFEFF/, ""));
        setWidget(node, "srt_path", "");
        update(text, `${file.name} · ${(file.size / 1024).toFixed(1)} KiB · 仅本地读取`);
    };
    node.addDOMWidget("indextts_srt_ui", "div", root, { serialize: false });
    node.setSize([Math.max(node.size[0], 600), Math.max(node.size[1], 620)]);
    setTimeout(() => update(widget(node, "srt_text")?.value || ""), 0);
}

app.registerExtension({
    name: "ComfyUI.IndexTTS25Local.SRTUI",
    async beforeRegisterNodeDef(nodeType, nodeData) {
        if (!NODE_TYPES.has(nodeData.name)) return;
        const original = nodeType.prototype.onNodeCreated;
        nodeType.prototype.onNodeCreated = function () {
            original?.apply(this, arguments);
            createPanel(this);
        };
    },
});
