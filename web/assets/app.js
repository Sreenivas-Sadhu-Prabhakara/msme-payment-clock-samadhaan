/* MSME 45-day payment clock — local UI.
   Talks only to this binary over same-origin fetch. No other network use. */
(function () {
  "use strict";

  /* ---------------- theme ---------------- */

  var root = document.documentElement;

  function storedTheme() {
    try { return localStorage.getItem("omsme-theme"); } catch (e) { return null; }
  }

  function currentTheme() {
    var set = root.getAttribute("data-theme");
    if (set) return set;
    return (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) ? "dark" : "light";
  }

  function paintThemer(button) {
    if (!button) return;
    var dark = currentTheme() === "dark";
    button.textContent = dark ? "☀ Light" : "☾ Dark";
    button.setAttribute("aria-label", dark ? "Switch to light theme" : "Switch to dark theme");
  }

  function wireTheme() {
    var button = document.querySelector("[data-themer]");
    if (!button) return;
    paintThemer(button);
    button.addEventListener("click", function () {
      var next = currentTheme() === "dark" ? "light" : "dark";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("omsme-theme", next); } catch (e) { /* private window */ }
      paintThemer(button);
    });
    if (window.matchMedia) {
      window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function () {
        if (!storedTheme()) paintThemer(button);
      });
    }
  }

  /* ---------------- helpers ---------------- */

  function $(id) { return document.getElementById(id); }

  var rupees = new Intl.NumberFormat("en-IN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

  function inr(value) {
    var n = Number(value);
    if (!isFinite(n)) n = 0;
    return "₹ " + rupees.format(n);
  }

  function amount(value) {
    var n = Number(value);
    if (!isFinite(n)) n = 0;
    return rupees.format(n);
  }

  function number(value) {
    var n = parseFloat(value);
    return isFinite(n) ? n : 0;
  }

  function el(tag, text, className) {
    var node = document.createElement(tag);
    if (text !== undefined && text !== null) node.textContent = text;
    if (className) node.className = className;
    return node;
  }

  function line(text) {
    var div = document.createElement("div");
    div.appendChild(el("small", text));
    return div;
  }

  var messageTimer = null;

  function showMessage(text, isError) {
    var box = $("message");
    box.textContent = text;
    box.hidden = false;
    if (messageTimer) window.clearTimeout(messageTimer);
    if (!isError) {
      messageTimer = window.setTimeout(function () { box.hidden = true; }, 6000);
    }
  }

  function clearMessage() {
    $("message").hidden = true;
  }

  function fail(err) {
    showMessage(err && err.message ? err.message : "Something went wrong.", true);
  }

  function api(path, options) {
    return fetch(path, options).then(function (res) {
      return res.text().then(function (raw) {
        var body = {};
        if (raw) {
          try { body = JSON.parse(raw); } catch (e) { body = {}; }
        }
        if (!res.ok) {
          throw new Error(body.error || ("The tool refused that (" + res.status + ")."));
        }
        return body;
      });
    });
  }

  function jsonPost(path, payload, method) {
    return api(path, {
      method: method || "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
  }

  /* ---------------- state ---------------- */

  var state = null;
  var editingId = null;
  var detailsLoaded = false;

  function asOfQuery() {
    var value = $("asOf").value;
    return value ? "?asOf=" + encodeURIComponent(value) : "";
  }

  function load() {
    return api("/api/state" + asOfQuery()).then(apply).catch(fail);
  }

  function apply(data) {
    state = data;
    if (!$("asOf").value && data.today) $("asOf").value = data.today;
    if (!detailsLoaded) {
      fillDetails(data);
      detailsLoaded = true;
    }
    render();
  }

  function fillDetails(data) {
    $("supplierName").value = data.supplier.name || "";
    $("udyamNumber").value = data.supplier.udyamNumber || "";
    $("supplierAddress").value = data.supplier.address || "";
    $("supplierContact").value = data.supplier.contact || "";
    $("supplierEmail").value = data.supplier.email || "";
    $("bankRatePct").value = data.bankRatePct;
  }

  /* ---------------- rendering ---------------- */

  function render() {
    if (!state) return;
    var sum = state.summary;

    $("totalDue").textContent = inr(sum.totalDue);
    $("sumPrincipal").textContent = amount(sum.outstandingPrincipal);
    $("sumInterest").textContent = amount(sum.interest);
    $("sumOverdue").textContent = String(sum.overdue);
    $("sumWithin").textContent = String(sum.within);
    $("sumSettled").textContent = String(sum.settled);
    $("sumTotal").textContent = amount(sum.totalDue);

    if (sum.count === 0) {
      $("totalSub").textContent = "No invoices on record yet.";
    } else if (sum.overdue === 0) {
      $("totalSub").textContent = "Nothing is past its appointed day as on " + state.asOf + ".";
    } else {
      $("totalSub").textContent = sum.overdue + " invoice(s) past the appointed day as on " + state.asOf + ".";
    }

    $("rateLine").textContent = "Statutory rate: " + state.interestMultiple + " × Bank Rate of " +
      state.bankRatePct.toFixed(2) + "% = " + state.annualRatePct.toFixed(2) +
      "% a year, compounded with monthly rests. The default is " +
      state.bankRateDefaultPct.toFixed(2) + "% — the Bank Rate changes, so check the current one.";

    renderRows();
    renderBuyers();
    tick();
  }

  function stateWord(status) {
    if (status.state === "overdue") return "Overdue";
    if (status.state === "settled") return "Settled";
    return "Within time";
  }

  function appointedLine(status) {
    if (!status.appointedDay) return "No acceptance date — the clock has not started";
    var basis = status.agreedInWriting ? "agreed in writing" : "15 days, no written agreement";
    if (status.cappedAt45Days) basis = "agreed date read down to 45 days";
    return "Appointed day " + status.appointedDay + " (" + basis + ")";
  }

  function clockCell(view) {
    var status = view.status;
    var cell = document.createElement("td");
    cell.appendChild(el("span", stateWord(status), "pill"));

    if (status.state === "overdue") {
      var over = el("div");
      over.appendChild(el("b", status.daysOverdue + " day(s) past"));
      cell.appendChild(over);
    } else if (status.state === "settled") {
      cell.appendChild(line(status.daysOverdue > 0
        ? "Paid " + status.daysOverdue + " day(s) late"
        : "Paid within time"));
    } else if (status.appointedDay) {
      cell.appendChild(line(status.daysLeft + " day(s) left"));
    }

    if (status.appointedDay && status.state !== "settled") {
      var live = el("small", "");
      live.setAttribute("data-deadline", status.appointedDay);
      var holder = el("div");
      holder.appendChild(live);
      cell.appendChild(holder);
    }
    return cell;
  }

  function amountsCell(view) {
    var status = view.status;
    var cell = el("td", null, "num");
    cell.appendChild(line("Invoice " + amount(view.amount)));
    if (view.amountReceived > 0) cell.appendChild(line("Received " + amount(view.amountReceived)));
    cell.appendChild(line("Unpaid " + amount(status.outstandingPrincipal)));
    cell.appendChild(line("Interest " + amount(status.interest)));
    var total = el("div");
    total.appendChild(el("b", amount(status.totalDue)));
    cell.appendChild(total);
    return cell;
  }

  function actionsCell(view) {
    var cell = document.createElement("td");
    var group = el("div", null, "seg");
    [
      ["reminder", "Reminder"],
      ["notice", "Notice"],
      ["edit", "Edit"],
      ["delete", "Delete"]
    ].forEach(function (pair) {
      var button = el("button", pair[1]);
      button.type = "button";
      button.setAttribute("data-act", pair[0]);
      button.setAttribute("data-id", view.id);
      group.appendChild(button);
    });
    cell.appendChild(group);
    return cell;
  }

  function renderRows() {
    var body = $("invoiceRows");
    body.textContent = "";
    var invoices = state.invoices || [];
    $("emptyNote").hidden = invoices.length > 0;
    $("invoiceTable").hidden = invoices.length === 0;

    invoices.forEach(function (view) {
      var tr = document.createElement("tr");

      var first = document.createElement("td");
      first.appendChild(el("b", view.no));
      first.appendChild(line(view.buyerName));
      if (view.buyerGstin) first.appendChild(line("GSTIN " + view.buyerGstin));
      first.appendChild(line("Accepted " + (view.acceptanceDate || "—")));
      first.appendChild(line(appointedLine(view.status)));

      tr.appendChild(first);
      tr.appendChild(clockCell(view));
      tr.appendChild(amountsCell(view));
      tr.appendChild(actionsCell(view));
      body.appendChild(tr);
    });
  }

  function renderBuyers() {
    var select = $("packetBuyer");
    var chosen = select.value;
    select.textContent = "";
    (state.buyers || []).forEach(function (name) {
      var option = document.createElement("option");
      option.value = name;
      option.textContent = name;
      select.appendChild(option);
    });
    if (chosen) select.value = chosen;
    $("packetCard").hidden = (state.buyers || []).length === 0;
  }

  /* ---------------- the live clock ---------------- */

  function pad(n) { return n < 10 ? "0" + n : String(n); }

  /* The appointed day is a whole day: payment on it is still in time, so the
     countdown runs to the end of that day. Once it has passed, the days are
     counted the way the statute counts them — from the appointed day itself —
     so the ticker and the day count beside it never disagree. */
  function liveText(dateText, now) {
    var parts = dateText.split("-");
    var year = Number(parts[0]), month = Number(parts[1]) - 1, day = Number(parts[2]);
    var start = new Date(year, month, day, 0, 0, 0, 0);
    var end = new Date(year, month, day, 23, 59, 59, 999);
    var past = now.getTime() > end.getTime();
    var ms = past ? now.getTime() - start.getTime() : end.getTime() - now.getTime();
    var seconds = Math.floor(Math.abs(ms) / 1000);
    var days = Math.floor(seconds / 86400);
    seconds -= days * 86400;
    var hours = Math.floor(seconds / 3600);
    seconds -= hours * 3600;
    var minutes = Math.floor(seconds / 60);
    seconds -= minutes * 60;
    return days + "d " + pad(hours) + ":" + pad(minutes) + ":" + pad(seconds) + (past ? " past" : " to go");
  }

  function tick() {
    if (!state) return;
    var live = state.asOf === state.today;
    var now = new Date();
    var nodes = document.querySelectorAll("[data-deadline]");
    for (var i = 0; i < nodes.length; i++) {
      var deadline = nodes[i].getAttribute("data-deadline");
      nodes[i].textContent = live ? liveText(deadline, now) : "as on " + state.asOf;
    }
  }

  /* ---------------- the invoice form ---------------- */

  function formPayload() {
    return {
      id: editingId || "",
      no: $("no").value,
      date: $("date").value,
      acceptanceDate: $("acceptanceDate").value,
      amount: number($("amount").value),
      buyerName: $("buyerName").value,
      buyerGstin: $("buyerGstin").value,
      agreedPaymentDate: $("agreedPaymentDate").value,
      amountReceived: number($("amountReceived").value),
      receivedDate: $("receivedDate").value
    };
  }

  function resetForm() {
    editingId = null;
    $("invoiceForm").reset();
    $("formTitle").textContent = "Add an invoice";
    $("save").textContent = "Add invoice";
    $("cancelEdit").hidden = true;
  }

  function startEdit(view) {
    editingId = view.id;
    $("no").value = view.no;
    $("date").value = view.date || "";
    $("acceptanceDate").value = view.acceptanceDate || "";
    $("amount").value = view.amount;
    $("buyerName").value = view.buyerName;
    $("buyerGstin").value = view.buyerGstin || "";
    $("agreedPaymentDate").value = view.agreedPaymentDate || "";
    $("amountReceived").value = view.amountReceived ? view.amountReceived : "";
    $("receivedDate").value = view.receivedDate || "";
    $("formTitle").textContent = "Edit invoice " + view.no;
    $("save").textContent = "Save changes";
    $("cancelEdit").hidden = false;
    $("formCard").scrollIntoView({ block: "start" });
  }

  function findInvoice(id) {
    var invoices = (state && state.invoices) || [];
    for (var i = 0; i < invoices.length; i++) {
      if (invoices[i].id === id) return invoices[i];
    }
    return null;
  }

  /* ---------------- documents ---------------- */

  function showDocument(doc) {
    $("docTitle").textContent = doc.title;
    $("docText").textContent = doc.text;
    $("docCard").hidden = false;
    $("docCard").scrollIntoView({ block: "start" });
  }

  function requestDocument(kind, invoiceId, buyer) {
    clearMessage();
    jsonPost("/api/documents", {
      kind: kind,
      invoiceId: invoiceId || "",
      buyer: buyer || "",
      asOf: $("asOf").value
    }).then(showDocument).catch(fail);
  }

  var printHidden = [];

  function hideForPrint() {
    printHidden = [];
    var blocks = document.querySelectorAll("main section, main > .wrap > .note");
    for (var i = 0; i < blocks.length; i++) {
      var block = blocks[i];
      if (block === $("docCard") || block.hidden) continue;
      block.hidden = true;
      printHidden.push(block);
    }
  }

  function restoreAfterPrint() {
    for (var i = 0; i < printHidden.length; i++) printHidden[i].hidden = false;
    printHidden = [];
  }

  function printDocument() {
    hideForPrint();
    window.addEventListener("afterprint", restoreAfterPrint, { once: true });
    window.setTimeout(restoreAfterPrint, 2000);
    window.print();
  }

  function copyDocument() {
    var text = $("docText").textContent;
    if (!navigator.clipboard || !navigator.clipboard.writeText) {
      showMessage("Select the text and copy it with your keyboard.", true);
      return;
    }
    navigator.clipboard.writeText(text).then(function () {
      showMessage("Copied. Paste it into your letterhead.", false);
    }).catch(function () {
      showMessage("Could not copy — select the text and copy it with your keyboard.", true);
    });
  }

  /* ---------------- wiring ---------------- */

  function wire() {
    $("invoiceForm").addEventListener("submit", function (event) {
      event.preventDefault();
      clearMessage();
      var payload = formPayload();
      var path = editingId
        ? "/api/invoices/" + encodeURIComponent(editingId) + asOfQuery()
        : "/api/invoices" + asOfQuery();
      jsonPost(path, payload, editingId ? "PUT" : "POST").then(function (data) {
        var saved = editingId;
        resetForm();
        apply(data);
        showMessage(saved ? "Invoice updated." : "Invoice added to the clock.", false);
      }).catch(fail);
    });

    $("cancelEdit").addEventListener("click", function () {
      resetForm();
      clearMessage();
    });

    $("clearForm").addEventListener("click", function () {
      window.setTimeout(resetForm, 0);
    });

    $("settingsForm").addEventListener("submit", function (event) {
      event.preventDefault();
      clearMessage();
      jsonPost("/api/settings" + asOfQuery(), {
        supplier: {
          name: $("supplierName").value,
          udyamNumber: $("udyamNumber").value,
          address: $("supplierAddress").value,
          contact: $("supplierContact").value,
          email: $("supplierEmail").value
        },
        bankRatePct: number($("bankRatePct").value)
      }).then(function (data) {
        apply(data);
        fillDetails(data);
        showMessage("Your details and the Bank Rate are saved.", false);
      }).catch(fail);
    });

    $("asOf").addEventListener("change", function () {
      load();
    });

    $("invoiceRows").addEventListener("click", function (event) {
      var button = event.target.closest("button[data-act]");
      if (!button) return;
      var id = button.getAttribute("data-id");
      var act = button.getAttribute("data-act");
      var view = findInvoice(id);
      if (!view) return;

      if (act === "edit") {
        startEdit(view);
        return;
      }
      if (act === "delete") {
        if (!window.confirm("Remove invoice " + view.no + " from the clock? This only changes your own records.")) return;
        api("/api/invoices/" + encodeURIComponent(id) + asOfQuery(), { method: "DELETE" })
          .then(function (data) {
            if (editingId === id) resetForm();
            apply(data);
            showMessage("Invoice removed.", false);
          }).catch(fail);
        return;
      }
      requestDocument(act, id, "");
    });

    $("buildPacket").addEventListener("click", function () {
      requestDocument("samadhaan", "", $("packetBuyer").value);
    });

    $("printDoc").addEventListener("click", printDocument);
    $("copyDoc").addEventListener("click", copyDocument);
    $("closeDoc").addEventListener("click", function () {
      $("docCard").hidden = true;
    });
  }

  wireTheme();
  wire();
  load();
  window.setInterval(tick, 1000);
})();
