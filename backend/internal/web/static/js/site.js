// Small progressive enhancements for the public site. Everything works without this file.
(function () {
  "use strict";

  // The home page header sits on the hero photo and turns solid once the page scrolls.
  var header = document.querySelector("[data-header].header-over-hero");
  if (header) {
    var onScroll = function () { header.classList.toggle("is-stuck", window.scrollY > 40); };
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
  }

  // Sections fade up as they scroll into view. Only sections that start below the fold are
  // hidden, so nothing on screen at load flickers, and reduced-motion users see no movement.
  if (!("IntersectionObserver" in window) || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  var targets = document.querySelectorAll("[data-reveal]");
  var io = new IntersectionObserver(function (entries) {
    entries.forEach(function (e) {
      if (e.isIntersecting) {
        e.target.classList.add("is-visible");
        io.unobserve(e.target);
      }
    });
  }, { rootMargin: "0px 0px -10% 0px" });
  targets.forEach(function (el) {
    if (el.getBoundingClientRect().top < window.innerHeight) return;
    el.classList.add("reveal");
    Array.prototype.forEach.call(el.querySelectorAll("[data-reveal-item]"), function (child, i) {
      child.classList.add("reveal");
      child.style.setProperty("--i", String(i % 6));
      io.observe(child);
    });
    io.observe(el);
  });
})();
