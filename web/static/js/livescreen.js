// The live QR screen's only script (M16-20, ADR 0045 §2.5). It does ONE thing: keep the code on
// this screen current, and hide it the moment it is not.
//
// HOW. It asks GET /screen/code for the next code, draws the answer into the empty drawing the
// server rendered (an SVG path and its side -- the drawing of the code, never the token as text),
// and asks again shortly before that code runs out. The answer says how long the drawn code counts
// (expiresIn); if that time passes without a newer answer -- the network is down, the server is
// unreachable -- the code is HIDDEN and the screen says it is reconnecting. So an offline screen
// never goes on showing an old code (the card). Time is measured with the page's own monotonic
// clock, so a wrong wall clock on the tablet changes nothing, and it is counted from the moment the
// request was SENT, not from the answer: a slow network shortens the display, never lengthens it.
//
// A switched-off venue or a paused business answers 409 with its word: the code is hidden, the
// server's sentence for that word is shown, and the page asks again every minute -- so the code
// comes back by itself when the poll answers one.
//
// WHAT IT DOES NOT DO:
//   - It writes no markup and no text. It sets two attributes on the drawing (viewBox and the path)
//     and flips `hidden` on three blocks the server rendered. The path is checked against the shape
//     the server writes before it is used.
//   - It stores nothing and talks to no other origin. The screen's cookie is HttpOnly; this script
//     never sees it.
//   - A removed, unpaired or run-out screen is answered by reloading the page, which then says so
//     in the server's words.
(function () {
  'use strict';

  var root = document.querySelector('[data-live-screen]');
  if (!root || !window.fetch) return;

  var url = root.getAttribute('data-code-url');
  var box = root.querySelector('[data-qr]');
  var svg = box && box.querySelector('svg');
  var path = svg && svg.querySelector('path');
  var wait = root.querySelector('[data-qr-wait]');
  var stale = root.querySelector('[data-qr-stale]');
  var states = root.querySelectorAll('[data-qr-state]');
  if (!url || !box || !svg || !path || !wait || !stale) return;

  // After a failed ask, try again this soon -- the drawn code is still shown until it runs out.
  var RETRY_MS = 3000;
  // A paused screen (a switched-off venue, a paused business) asks this often.
  var PAUSED_MS = 60000;
  // The shape SVGPath writes: M<x> <y>h<n>v1h-<n>z, repeated.
  var PATH_SHAPE = /^(M\d+ \d+h\d+v1h-\d+z)+$/;

  var deadline = 0;
  var timer = null;

  function now() {
    return window.performance && performance.now ? performance.now() : Date.now();
  }

  function showState(word) {
    for (var i = 0; i < states.length; i++) {
      states[i].hidden = states[i].getAttribute('data-qr-state') !== word;
    }
  }

  function showCode(body, sentAt) {
    svg.setAttribute('viewBox', '0 0 ' + body.side + ' ' + body.side);
    path.setAttribute('d', body.d);
    deadline = sentAt + body.expiresIn * 1000;
    box.hidden = false;
    wait.hidden = true;
    stale.hidden = true;
    showState('');
  }

  function pause(word) {
    deadline = 0;
    box.hidden = true;
    path.setAttribute('d', '');
    wait.hidden = true;
    stale.hidden = true;
    showState(word);
  }

  function hideCode() {
    box.hidden = true;
    path.setAttribute('d', '');
    wait.hidden = true;
    stale.hidden = false;
  }

  function schedule(ms) {
    if (timer !== null) window.clearTimeout(timer);
    timer = window.setTimeout(ask, Math.max(ms, 1000));
  }

  function valid(body) {
    return body && typeof body.d === 'string' && PATH_SHAPE.test(body.d) &&
      typeof body.side === 'number' && body.side >= 21 && body.side <= 200 &&
      typeof body.expiresIn === 'number' && body.expiresIn > 0 && body.expiresIn <= 120 &&
      typeof body.refreshIn === 'number';
  }

  function ask() {
    timer = null;
    var sentAt = now();
    window
      .fetch(url, { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } })
      .then(function (res) {
        if (res.status === 401 || res.status === 410) {
          // Not a network problem: the screen was removed, unpaired or its pairing ran out. The
          // page says which in its own words.
          window.location.reload();
          return null;
        }
        if (res.status === 409) {
          return res.json().then(function (b) {
            var word = b && (b.state === 'inactive' || b.state === 'paused') ? b.state : 'paused';
            pause(word);
            schedule(PAUSED_MS);
            return null;
          });
        }
        if (!res.ok) throw new Error('status ' + res.status);
        return res.json();
      })
      .then(function (body) {
        if (body === null) return;
        if (!valid(body)) throw new Error('shape');
        showCode(body, sentAt);
        schedule(body.refreshIn * 1000);
      })
      .catch(function () {
        schedule(RETRY_MS);
      });
  }

  // The watch: once a second, a code past its time is hidden, answer or no answer.
  window.setInterval(function () {
    if (deadline !== 0 && now() >= deadline && !box.hidden) hideCode();
  }, 1000);

  // Coming back to a sleeping tab is when the code is most likely stale: ask at once.
  document.addEventListener('visibilitychange', function () {
    if (document.visibilityState === 'visible') {
      if (deadline !== 0 && now() >= deadline) hideCode();
      schedule(0);
    }
  });

  ask();
})();
