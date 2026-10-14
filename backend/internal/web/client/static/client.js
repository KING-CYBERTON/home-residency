// Small progressive enhancements for the client portal. Every page works without this file.
// The CSP allows no inline script, so every behaviour hangs off a data- attribute here.
(function () {
  "use strict";
  var meta = function (n) { var m = document.querySelector('meta[name="' + n + '"]'); return m ? m.content : ""; };
  var base = meta("portal-base") || "/app";

  // Installable app.
  if ("serviceWorker" in navigator) {
    window.addEventListener("load", function () { navigator.serviceWorker.register(base + "/sw.js", { scope: base + "/" }); });
  }
  var deferred;
  window.addEventListener("beforeinstallprompt", function (e) {
    e.preventDefault();
    deferred = e;
    document.querySelectorAll("[data-install]").forEach(function (b) { b.hidden = false; });
  });
  document.addEventListener("click", function (e) {
    var b = e.target.closest("[data-install]");
    if (!b || !deferred) return;
    deferred.prompt();
    deferred.userChoice.finally(function () { deferred = null; b.hidden = true; });
  });

  // Signing out also forgets the pages kept for offline use on this phone.
  document.addEventListener("submit", function (e) {
    if (e.target.matches("[data-clear-cache]") && window.caches) {
      caches.keys().then(function (keys) { keys.forEach(function (k) { if (k.indexOf("client-") === 0) caches.delete(k); }); });
    }
  }, true);

  document.addEventListener("click", function (e) {
    if (e.target.closest("[data-reload]")) location.reload();
    var geo = e.target.closest("[data-geolocate]");
    if (geo && navigator.geolocation) {
      var f = geo.form, note = f.querySelector("[data-geo]");
      navigator.geolocation.getCurrentPosition(function (p) {
        f.elements.lat.value = p.coords.latitude.toFixed(6);
        f.elements.lng.value = p.coords.longitude.toFixed(6);
        if (note) note.textContent = "Location saved.";
      }, function () { if (note) note.textContent = "Could not get your location. You can still save the address."; });
    }
  });

  // Uploads go straight to the document vault with a presigned URL:
  //   <input type="file" data-upload="national_id" data-target="#document_id">
  async function upload(input) {
    var file = input.files[0];
    var status = input.parentElement.querySelector("[data-upload-status]");
    var target = document.querySelector(input.dataset.target);
    var say = function (t) { if (status) status.textContent = t; };
    if (!file) return;
    if (file.size > 10 * 1024 * 1024) { say("That file is over 10 MB. Try a smaller photo."); input.value = ""; return; }
    say("Uploading…");
    var form = new FormData();
    form.append("document_type", input.dataset.upload);
    form.append("mime_type", file.type);
    form.append("size_bytes", file.size);
    form.append("filename", file.name);
    try {
      var headers = {};
      var res = await fetch(base + "/uploads", { method: "POST", body: form, headers: headers, credentials: "same-origin" });
      var ticket = await res.json();
      if (!res.ok) throw new Error(ticket.message);
      var put = await fetch(ticket.upload_url, { method: ticket.upload_method || "PUT", body: file, headers: ticket.upload_headers || {} });
      if (!put.ok) throw new Error("The upload did not go through.");
      res = await fetch(base + "/uploads/" + ticket.document.id + "/complete", { method: "POST", headers: headers, credentials: "same-origin" });
      var doc = await res.json();
      if (!res.ok) throw new Error(doc.message);
      if (input.multiple || target.dataset.many !== undefined) {
        target.value = target.value ? target.value + "," + doc.id : doc.id;
      } else {
        target.value = doc.id;
      }
      say("Uploaded " + file.name);
    } catch (err) {
      say((err && err.message) || "The upload failed. Check your connection and try again.");
      input.value = "";
    }
  }
  document.addEventListener("change", function (e) {
    if (e.target.matches("input[type=file][data-upload]")) upload(e.target);
  });
  // Web push: the Notifications page shows [data-push] (the VAPID public key) when the server has one.
  function vapidKey(b64) {
    var s = (b64 + "===".slice((b64.length + 3) % 4)).replace(/-/g, "+").replace(/_/g, "/");
    var raw = atob(s), out = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
    return out;
  }
  function pushPanels() {
    if (!("serviceWorker" in navigator) || !("PushManager" in window)) return;
    document.querySelectorAll("[data-push]").forEach(function (box) {
      navigator.serviceWorker.ready.then(function (reg) {
        return reg.pushManager.getSubscription();
      }).then(function (sub) {
        if (!sub || Notification.permission !== "granted") box.classList.remove("hidden");
      });
    });
  }
  function pushOn(box) {
    var msg = box.querySelector("[data-push-msg]");
    Notification.requestPermission().then(function (perm) {
      if (perm !== "granted") throw new Error("Notifications are blocked for this site in your browser settings.");
      return navigator.serviceWorker.ready;
    }).then(function (reg) {
      return reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: vapidKey(box.dataset.push) });
    }).then(function (sub) {
      return fetch(box.dataset.pushUrl, {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(sub.toJSON())
      });
    }).then(function (res) {
      if (!res.ok) throw new Error("Could not turn on notifications. Try again.");
      box.classList.add("hidden");
    }).catch(function (err) {
      msg.textContent = err.message || "Could not turn on notifications.";
    });
  }
  document.addEventListener("click", function (e) {
    var box = e.target.closest("[data-push] button");
    if (box) pushOn(box.closest("[data-push]"));
  });
  pushPanels();
  document.addEventListener("htmx:afterSettle", pushPanels);
})();
