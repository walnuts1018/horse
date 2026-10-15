const CACHE_NAME = "horse-static-v2";
const OFFLINE_URL = "/offline.html";
const OFFLINE_SCRIPT = "/offline-shell.js";
const API_PATHS = ["/graphql", "/auth/", "/events", "/documents", "/purchases/"];
const STATIC_DESTINATIONS = new Set(["script", "style", "font", "image"]);

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE_NAME)
      .then((cache) => cache.addAll([OFFLINE_URL, OFFLINE_SCRIPT, "/icons/horse-192.png"])),
  );
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter((key) => key.startsWith("horse-static-") && key !== CACHE_NAME)
            .map((key) => caches.delete(key)),
        ),
      )
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== "GET" || url.origin !== self.location.origin) return;
  if (API_PATHS.some((path) => url.pathname === path || url.pathname.startsWith(path))) return;

  if (request.mode === "navigate") {
    event.respondWith(fetch(request).catch(() => caches.match(OFFLINE_URL)));
    return;
  }

  if (
    !STATIC_DESTINATIONS.has(request.destination) ||
    !(url.pathname.startsWith("/assets/") || url.pathname.startsWith("/icons/"))
  )
    return;
  event.respondWith(
    caches.open(CACHE_NAME).then(async (cache) => {
      try {
        const response = await fetch(request);
        if (response.ok && response.type === "basic") await cache.put(request, response.clone());
        return response;
      } catch {
        const cached = await cache.match(request);
        if (cached) return cached;
        throw new Error("Static resource is unavailable offline");
      }
    }),
  );
});
