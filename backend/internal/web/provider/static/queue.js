/* Offline queue for check-in and check-out (ADR-0001). Loaded by portal.js and by the service worker,
   so a page or a background sync can send what was saved while the phone had no signal. Events keep
   the time they happened on the phone; the server applies each one once (client_event_id). */
(function (g) {
  var DB = 'hr-provider', EVENTS = 'events', FAILED = 'failed';

  function open() {
    return new Promise(function (res, rej) {
      var q = indexedDB.open(DB, 1);
      q.onupgradeneeded = function () {
        q.result.createObjectStore(EVENTS, { keyPath: 'client_event_id' });
        q.result.createObjectStore(FAILED, { keyPath: 'client_event_id' });
      };
      q.onsuccess = function () { res(q.result); };
      q.onerror = function () { rej(q.error); };
    });
  }

  function tx(store, mode, fn) {
    return open().then(function (db) {
      return new Promise(function (res, rej) {
        var t = db.transaction(store, mode), req = fn(t.objectStore(store));
        t.oncomplete = function () { res(req ? req.result : undefined); };
        t.onerror = function () { rej(t.error); };
      });
    });
  }

  var sending = null;

  var Q = {
    add: function (ev) { return tx(EVENTS, 'readwrite', function (s) { s.put(ev); }); },
    all: function () { return tx(EVENTS, 'readonly', function (s) { return s.getAll(); }); },
    failed: function () { return tx(FAILED, 'readonly', function (s) { return s.getAll(); }); },
    clearFailed: function () { return tx(FAILED, 'readwrite', function (s) { s.clear(); }); },

    // flush sends everything queued. It resolves with what was applied and what the server refused,
    // and rejects when there is no connection (the events stay queued) or the session has ended.
    flush: function () {
      if (sending) return sending;
      // The page and the service worker may both try at once; a Web Lock lets one send while the
      // other waits and then finds the queue empty, so no event is sent twice in parallel.
      var locks = self.navigator && self.navigator.locks;
      sending = locks ? locks.request('hr-sync', send) : send();
      var clear = function () { sending = null; };
      sending.then(clear, clear);
      return sending;
    }
  };

  function send() {
    return Q.all().then(function (evs) {
      if (!evs.length) return { applied: 0, rejected: [] };
      evs = evs.slice(0, 200);
      return fetch('/provider/sync', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
        body: JSON.stringify({ events: evs })
      }).then(function (r) {
        if (r.status === 401) { var e = new Error('signin'); e.signin = true; throw e; }
        if (!r.ok) throw new Error('sync ' + r.status);
        return r.json();
      }).then(function (body) {
        var byId = {}, done = [], rejected = [];
        evs.forEach(function (e) { byId[e.client_event_id] = e; });
        (body.results || []).forEach(function (x) {
          done.push(x.client_event_id);
          if (x.status === 'rejected') {
            rejected.push({ client_event_id: x.client_event_id, event: byId[x.client_event_id], error: (x.error && x.error.message) || 'Not accepted' });
          }
        });
        return tx(EVENTS, 'readwrite', function (s) { done.forEach(function (id) { s.delete(id); }); })
          .then(function () {
            if (rejected.length) return tx(FAILED, 'readwrite', function (s) { rejected.forEach(function (x) { s.put(x); }); });
          })
          .then(function () { return { applied: done.length - rejected.length, rejected: rejected }; });
      });
    });
  }

  g.hrQueue = Q;
})(self);
