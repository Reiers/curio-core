// Curio Core - animated gradient background
// Vanilla JS port of the framer-motion + radial-gradient component.
// Greyscale-dominant palette so the UI stays monochrome; one cool
// brand-teal accent at the deepest stop. Subtle "breathing" animation
// drives a slow opacity/scale shift through the radial gradient's
// outer extent.

(function () {
  'use strict';

  function mount(el, opts) {
    var startingGap   = opts.startingGap   || 140;
    var breathingRange = opts.breathingRange || 6;
    var animationSpeed = opts.animationSpeed || 0.015;
    var breathing     = opts.breathing !== false;
    var topOffset     = opts.topOffset || 0;
    var colors = opts.colors || [
      // Deep core -> charcoal -> cool steel -> brand teal halo ->
      // soft cyan ring -> back to near-black at the corners.
      // Tuned for visibility on a black surface without overpowering
      // the foreground chrome.
      '#050505',
      '#0a0e14',
      '#0e1a22',
      '#0d2a36',
      '#0f4248',
      '#22BFC4',
      '#0c0c10',
      '#000000'
    ];
    var stops = opts.stops || [0, 20, 38, 55, 70, 82, 92, 100];
    if (colors.length !== stops.length) {
      // Defensive: silently truncate to min length so we never throw
      // in production.
      var n = Math.min(colors.length, stops.length);
      colors = colors.slice(0, n);
      stops = stops.slice(0, n);
    }

    var width = startingGap;
    var dir = 1;
    var raf = 0;

    function tick() {
      if (breathing) {
        if (width >= startingGap + breathingRange) dir = -1;
        if (width <= startingGap - breathingRange) dir = 1;
        width += dir * animationSpeed;
      }
      var stopsStr = stops.map(function (s, i) { return colors[i] + ' ' + s + '%'; }).join(', ');
      el.style.background =
        'radial-gradient(' + width.toFixed(3) + '% ' +
        (width + topOffset).toFixed(3) + '% at 50% 25%, ' + stopsStr + ')';
      raf = requestAnimationFrame(tick);
    }

    // Respect prefers-reduced-motion: render a static gradient.
    var mqr = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)');
    if (mqr && mqr.matches) {
      breathing = false;
    }

    raf = requestAnimationFrame(tick);

    // Pause when the tab is hidden.
    document.addEventListener('visibilitychange', function () {
      if (document.hidden) {
        cancelAnimationFrame(raf);
      } else {
        raf = requestAnimationFrame(tick);
      }
    });
  }

  function init() {
    var el = document.querySelector('.bg-gradient');
    if (!el) return;
    mount(el, {});
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
