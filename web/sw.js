// Chrome 85 requires a fetch handler for the page to be installable. No caching: always hit the server.
self.addEventListener("fetch", function (event) {
  event.respondWith(fetch(event.request));
});
