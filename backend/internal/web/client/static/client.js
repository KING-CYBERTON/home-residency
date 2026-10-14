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
})();
