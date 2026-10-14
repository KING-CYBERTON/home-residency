// Service worker for the Homend client portal. __BASE__ is replaced with the mount path when served.
// Pages are network-first (bookings and payments must be live); the last copy of each page is kept so
// a household on a patchy connection can still see their bookings, and the offline page covers the rest.
const VERSION = "client-v1";
const BASE = "__BASE__";
const SHELL = [BASE + "/offline", BASE + "/static/client.js", BASE + "/manifest.webmanifest"];

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(VERSION).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith("client-") && k !== VERSION).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  const url = new URL(req.url);
  if (req.method !== "GET" || url.origin !== self.location.origin) return;

  // Static files: the shared ones (/static/, cache-busted URLs) and the portal's own. Cache first, so
  // pages kept for offline use still have their styles.
  if (url.pathname.startsWith("/static/") || url.pathname.startsWith(BASE + "/static/")) {
    event.respondWith(caches.match(req).then((hit) => hit || fetch(req).then((res) => {
      const copy = res.clone();
      caches.open(VERSION).then((c) => c.put(req, copy));
      return res;
    })));
    return;
  }

  if (!url.pathname.startsWith(BASE + "/")) return;

  // Full page loads: network first, then the last copy, then the offline page. HTMX fragments and
  // payment polling are never served from cache.
  if (req.mode === "navigate" && !req.headers.get("HX-Request")) {
    event.respondWith(
      fetch(req).then((res) => {
        if (res.ok && !url.pathname.startsWith(BASE + "/payments/")) {
          const copy = res.clone();
          caches.open(VERSION).then((c) => c.put(req, copy));
        }
        return res;
      }).catch(() => caches.match(req).then((hit) => hit || caches.match(BASE + "/offline")))
    );
  }
});

// Web push: the API stores subscriptions (POST /api/v1/push-subscriptions); show what it sends.
self.addEventListener("push", (event) => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch (e) { data = { body: event.data && event.data.text() }; }
  event.waitUntil(self.registration.showNotification(data.title || "Homend Residence", {
    body: data.body || "",
    icon: "/static/img/icon-192.png",
    badge: "/static/img/icon-192.png",
    data: { url: data.url || BASE + "/notifications" },
  }));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(self.clients.openWindow(event.notification.data.url));
});
