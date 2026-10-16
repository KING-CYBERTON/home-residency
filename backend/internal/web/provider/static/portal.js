/* Provider portal behaviour. Every page works without this file; it adds the offline queue for
   check-in and check-out, location for geo check-in, direct uploads to the document vault and
   confirmation prompts. The CSP allows no inline script, so all behaviour hangs off data attributes. */
(function () {
  var Q = self.hrQueue;
  var $ = function (sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); };

  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/provider/sw.js', { scope: '/provider/' }).catch(function () {});
  }

  function uuid() {
    if (self.crypto && crypto.randomUUID) return crypto.randomUUID();
    var b = crypto.getRandomValues(new Uint8Array(16));
    b[6] = (b[6] & 15) | 64; b[8] = (b[8] & 63) | 128;
    var h = Array.prototype.map.call(b, function (x) { return (x + 256).toString(16).slice(1); }).join('');
    return h.slice(0, 8) + '-' + h.slice(8, 12) + '-' + h.slice(12, 16) + '-' + h.slice(16, 20) + '-' + h.slice(20);
  }

  function say(text, isError) {
    var box = document.getElementById('hr-sync');
    if (!box) return;
    box.textContent = text;
    box.className = 'mx-auto mb-3 max-w-xl rounded-md px-3 py-2 text-sm ' + (isError ? 'bg-red-50 text-red-800' : 'bg-amber-50 text-amber-900');
    box.hidden = !text;
  }

  // ---- visits: show what is saved on the phone but not yet sent

  function showPending() {
    if (!Q) return Promise.resolve();
    return Q.all().then(function (evs) {
      evs.sort(function (a, b) { return a.device_recorded_at < b.device_recorded_at ? -1 : 1; });
      evs.forEach(function (ev) {
        $('[data-visit="' + ev.visit_id + '"]').forEach(function (card) {
          var tag = card.querySelector('[data-pending]');
          if (tag) { tag.hidden = false; tag.textContent = (ev.event_type === 'check_in' ? 'Checked in' : 'Checked out') + ' on this phone, waiting to send'; }
          var ci = card.querySelector('[data-offline="check_in"]'), co = card.querySelector('[data-offline="check_out"]');
          if (ev.event_type === 'check_in') { if (ci) ci.hidden = true; if (co) co.hidden = false; }
          if (ev.event_type === 'check_out') { if (ci) ci.hidden = true; if (co) co.hidden = true; }
        });
      });
      if (evs.length) say(evs.length + (evs.length === 1 ? ' check-in or check-out is' : ' check-ins and check-outs are') + ' saved on this phone and will be sent when you have a connection.');
      return Q.failed();
    }).then(function (bad) {
      if (bad && bad.length) {
        say('Not accepted: ' + bad.map(function (x) { return x.error; }).join(' ') + ' Please call the office if you need help.', true);
        bad.forEach(function (x) { if (x.event) restore(x.event); });
        Q.clearFailed();
      }
    });
  }

  // A refused event leaves the visit as the server has it: the pending note goes and the form it
  // came from shows again, so the provider can retry (for example with the right door code).
  function restore(ev) {
    $('[data-visit="' + ev.visit_id + '"]').forEach(function (card) {
      var tag = card.querySelector('[data-pending]');
      if (tag) { tag.hidden = true; tag.textContent = ''; }
      var ci = card.querySelector('[data-offline="check_in"]'), co = card.querySelector('[data-offline="check_out"]');
      if (ev.event_type === 'check_in') { if (ci) ci.hidden = false; if (co) co.hidden = true; }
      if (ev.event_type === 'check_out' && co) co.hidden = false;
      var fresh = card.querySelector('[data-offline="' + ev.event_type + '"] [name=client_event_id]');
      if (fresh) fresh.value = uuid(); // a retry is a new event, not a replay of the refused one
    });
  }

  function synced(res) {
    if (res.rejected.length) {
      say('Not accepted: ' + res.rejected.map(function (x) { return x.error; }).join(' '), true);
      res.rejected.forEach(function (x) { if (x.event) restore(x.event); });
      Q.clearFailed();
    } else if (res.applied) {
      say('');
      if (document.querySelector('[data-visit]')) location.reload();
    }
  }

  // The service worker may send the queue in the background (Background Sync) and reports back here.
  if (navigator.serviceWorker) {
    navigator.serviceWorker.addEventListener('message', function (e) {
      if (e.data && e.data.type === 'hr-synced') synced(e.data);
    });
  }

  function flush() {
    if (!Q || !navigator.onLine) return;
    Q.flush().then(synced, function (err) {
      if (err && err.signin) say('Sign in again to send what is saved on this phone.', true);
    });
  }

  function position() {
    return new Promise(function (res, rej) {
      if (!navigator.geolocation) return rej(new Error('Location is not available on this phone. Use the door code.'));
      navigator.geolocation.getCurrentPosition(function (p) { res(p.coords); },
        function () { rej(new Error('We could not get your location. Turn on location, or use the door code.')); },
        { enableHighAccuracy: true, timeout: 15000, maximumAge: 60000 });
    });
  }

  $('form[data-offline]').forEach(function (form) {
    form.addEventListener('submit', function (e) {
      if (!Q) return; // no IndexedDB: the plain form post still works online
      e.preventDefault();
      var f = new FormData(form), type = form.getAttribute('data-offline');
      var method = f.get('method') || 'code';
      var ev = {
        client_event_id: f.get('client_event_id') || uuid(),
        visit_id: form.getAttribute('data-visit-id'),
        event_type: type,
        device_recorded_at: new Date().toISOString(),
        payload: {}
      };
      var btn = form.querySelector('button[type=submit]');
      if (btn) btn.disabled = true;
      var ready = Promise.resolve();
      if (type === 'check_in') {
        ev.payload.method = method;
        if (method === 'code') ev.payload.code = String(f.get('code') || '').trim();
        if (method === 'geo') ready = position().then(function (c) { ev.payload.lat = c.latitude; ev.payload.lng = c.longitude; });
      } else {
        var notes = String(f.get('notes') || '').trim();
        if (notes) ev.payload.notes = notes;
      }
      ready.then(function () { return Q.add(ev); }).then(function () {
        if (navigator.serviceWorker && navigator.serviceWorker.ready) {
          navigator.serviceWorker.ready.then(function (reg) { if (reg.sync) return reg.sync.register('hr-sync'); }).catch(function () {}); // refused where Background Sync is off; the page sends instead
        }
        return showPending();
      }).then(flush, function (err) {
        say(err.message, true);
      }).then(function () { if (btn) btn.disabled = false; });
    });
  });

  // Geo check-in needs no door code; show the code box only when it is wanted.
  $('[data-method]').forEach(function (radio) {
    radio.addEventListener('change', function () {
      var box = radio.form && radio.form.querySelector('[data-code-box]');
      if (box) box.hidden = radio.value !== 'code';
    });
  });

  // ---- uploads go straight to the vault with a short-lived link, then the server confirms them

  // Upload with no file chosen opens the file picker, and choosing a file from there sends it, so the
  // button works whether or not the person noticed the separate "Choose file" control.
  $('form[data-upload]').forEach(function (form) {
    var input = form.querySelector('input[type=file]');
    var sendOnPick = false;
    input.addEventListener('change', function () {
      if (sendOnPick && input.files[0]) { sendOnPick = false; form.requestSubmit(); }
    });
    form.addEventListener('submit', function (e) {
      var file = input.files[0];
      var status = form.querySelector('[data-upload-status]');
      e.preventDefault();
      if (!file) { sendOnPick = true; status.textContent = 'Choose a photo or PDF.'; input.click(); return; }
      var f = new FormData(form);
      if (file.size > 10 * 1024 * 1024) { status.textContent = 'This file is bigger than 10 MB. Take a smaller photo.'; return; }
      var ticket = {
        document_type: f.get('document_type'), mime_type: file.type, size_bytes: file.size, original_filename: file.name
      };
      ['issued_on', 'expires_on', 'document_number'].forEach(function (k) { if (f.get(k)) ticket[k] = f.get(k); });
      status.textContent = 'Uploading…';
      fetch('/provider/documents/ticket', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(ticket) })
        .then(function (r) { return r.json().then(function (b) { if (!r.ok) throw new Error(b.message || 'Upload refused'); return b; }); })
        .then(function (t) {
          var headers = t.upload_headers || {};
          headers['Content-Type'] = file.type;
          return fetch(t.upload_url, { method: t.upload_method || 'PUT', headers: headers, body: file }).then(function (r) {
            if (!r.ok) throw new Error('The upload did not finish. Check your connection and try again.');
            return t.document.id;
          });
        })
        .then(function (id) {
          form.querySelector('input[type=file]').disabled = true;
          form.action = '/provider/documents/' + id + '/complete';
          form.submit();
        })
        .catch(function (err) { status.textContent = err.message; });
    });
  });

  // Signing out clears everything this portal keeps on the phone (Clear-Site-Data), including
  // check-ins not yet sent. Try to send them first, and ask before discarding any that remain.
  $('form[action="/provider/logout"]').forEach(function (form) {
    form.addEventListener('submit', function (e) {
      if (!Q || form.dataset.ready) return;
      e.preventDefault();
      var go = function () { form.dataset.ready = '1'; form.submit(); };
      (navigator.onLine ? Q.flush().catch(function () {}) : Promise.resolve())
        .then(function () { return Q.all(); })
        .then(function (left) {
          if (!left.length || confirm('Not sent yet: ' + left.length + ' check-in or check-out saved on this phone. Signing out deletes it. Sign out anyway?')) go();
        }, go);
    });
  });

  $('[data-confirm]').forEach(function (el) {
    var form = el.tagName === 'FORM' ? el : el.form;
    if (form) form.addEventListener('submit', function (e) { if (!confirm(el.getAttribute('data-confirm'))) e.preventDefault(); });
  });

  $('[data-needs-js]').forEach(function (el) { el.hidden = false; });

  showPending().then(flush);
  addEventListener('online', flush);
})();
