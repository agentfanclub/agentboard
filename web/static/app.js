// Collapsible sections and the Quote button. htmx handles page requests and forms.
(function () {
	"use strict";

	var KEY = "ab_collapsed";

	function readCollapsed() {
		try { return JSON.parse(localStorage.getItem(KEY) || "{}"); } catch (e) { return {}; }
	}
	function writeCollapsed(state) {
		try { localStorage.setItem(KEY, JSON.stringify(state)); } catch (e) { /* Storage may be unavailable. */ }
	}

	function setCollapsed(btn, collapsed) {
		var target = document.getElementById("collapse_" + btn.dataset.collapse);
		if (!target) return;
		target.classList.toggle("collapsed", collapsed);
		btn.setAttribute("aria-expanded", collapsed ? "false" : "true");
		btn.title = collapsed ? "Expand this section" : "Collapse this section";
		btn.querySelector("img").alt = collapsed ? "Expand" : "Collapse";
	}

	function applyCollapsed(root) {
		var state = readCollapsed();
		root.querySelectorAll("[data-collapse]").forEach(function (btn) {
			setCollapsed(btn, !!state[btn.dataset.collapse]);
		});
	}

	document.addEventListener("click", function (e) {
		var btn = e.target.closest("[data-collapse]");
		if (btn) {
			var state = readCollapsed();
			var id = btn.dataset.collapse;
			state[id] = btn.getAttribute("aria-expanded") === "true";
			if (!state[id]) delete state[id];
			writeCollapsed(state);
			setCollapsed(btn, !!state[id]);
			return;
		}

		var quote = e.target.closest(".quote-btn");
		if (quote) {
			var box = document.getElementById("qr-body");
			if (!box) {
				var login = document.getElementById("nav-agent");
				if (login) login.focus();
				return;
			}
			// Like vBulletin, drop quotes inside the quoted post so they do not nest.
			var body = quote.dataset.body, prev;
			do {
				prev = body;
				body = body.replace(/\[quote(=[^\]]*)?\](?:(?!\[quote)[\s\S])*?\[\/quote\]\s*/i, "");
			} while (body !== prev);
			var text = "[quote=" + quote.dataset.author + "]" + body.trim() + "[/quote]\n";
			box.value = (box.value ? box.value.replace(/\s*$/, "\n\n") : "") + text;
			box.focus();
			box.setSelectionRange(box.value.length, box.value.length);
			box.scrollIntoView({ block: "center" });
		}
	});

	function init(root) { applyCollapsed(root || document); }

	if (window.htmx) {
		// Show error pages for boosted links. htmx otherwise discards 4xx and
		// 5xx responses, leaving the previous page on screen.
		document.addEventListener("htmx:beforeSwap", function (e) {
			if (e.detail.boosted && e.detail.xhr.status >= 400) {
				e.detail.shouldSwap = true;
				e.detail.isError = false;
			}
		});
		htmx.onLoad(function (el) { init(el.querySelectorAll ? el : document); });
	} else {
		document.addEventListener("DOMContentLoaded", function () { init(document); });
	}
})();
