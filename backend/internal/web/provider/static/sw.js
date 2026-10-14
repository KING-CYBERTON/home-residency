/* Provider portal service worker. Keeps the shell, today's jobs and visited job pages for offline use,
   and sends queued check-ins and check-outs when the connection returns (Background Sync where the
   browser has it; portal.js also retries on load and when the phone comes back online). */
importScripts('/provider/static/queue.js');

var ASSETS = self.HR_ASSETS;
var VERSION = 'hr-provider-' + ASSETS.join('|').split('').reduce(function (h, c) { return (h * 31 + c.charCodeAt(0)) | 0; }, 0);
// Pages worth having offline: home, the jobs list and each job (address, access details, visit forms).
var KEEP = /^\/provider\/(jobs(\/[0-9a-f-]{36})?)?$/;

self.addEventListener('install', function (e) {
  e.waitUntil(caches.open(VERSION).then(function (c) { return c.addAll(ASSETS); }).then(function () { return self.skipWaiting(); }));
});

self.addEventListener('activate', function (e) {
  e.waitUntil(caches.keys().then(function (keys) {
    return Promise.all(keys.filter(function (k) { return k !== VERSION; }).map(function (k) { return caches.delete(k); }));
  }).then(function () { return self.clients.claim(); }));
});

self.addEventListener('fetch', function (e) {
  var req = e.request, url = new URL(req.url);
  if (req.method !== 'GET' || url.origin !== self.location.origin) return;

  if (url.pathname.indexOf('/static/') === 0 || url.pathname.indexOf('/provider/static/') === 0) {
    e.respondWith(caches.match(req).then(function (hit) {
      return hit || fetch(req).then(function (res) {
        if (res.ok) { var copy = res.clone(); caches.open(VERSION).then(function (c) { c.put(req, copy); }); }
        return res;
      });
    }));
    return;
  }

  if (req.mode === 'navigate' && url.pathname.indexOf('/provider') === 0) {
    e.respondWith(fetch(req).then(function (res) {
      if (res.ok && !res.redirected && KEEP.test(url.pathname)) {
        var copy = res.clone();
        caches.open(VERSION).then(function (c) { c.put(url.pathname, copy); });
      }
      return res;
    }).catch(function () {
      return caches.match(url.pathname).then(function (hit) { return hit || caches.match('/provider/offline'); });
    }));
  }
});

// Background sync: send the queue, then tell open pages so they can show the result.
self.addEventListener('sync', function (e) {
  if (e.tag !== 'hr-sync') return;
  e.waitUntil(self.hrQueue.flush().then(function (res) {
    return self.clients.matchAll({ type: 'window' }).then(function (list) {
      list.forEach(function (c) { c.postMessage({ type: 'hr-synced', applied: res.applied, rejected: res.rejected }); });
    });
  }));
});
