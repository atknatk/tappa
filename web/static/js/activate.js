// The activation waiting screen's only script (ADR 0026). It does ONE thing:
// notice that the activation finished in ANOTHER TAB and say so here.
//
// WHY THERE IS ANOTHER TAB. The first NFC tap completes the activation, and a tap
// opens the phone's default browser on a fresh page. The session cookie it sets
// is shared by every tab of that browser, so this page can ask the server —
// GET /activate/status — whether that has happened, and swap itself to the
// "Activation complete" block that is already in its HTML, hidden.
//
// WHAT IT DOES NOT DO:
//
//   - It writes no markup and no text. It flips two `hidden` attributes and moves
//     focus; everything shown was rendered by the server. So there is nothing to
//     escape and the page's Content-Security-Policy needs no inline anything.
//   - It stores nothing (no localStorage, no cookie) and talks to no other origin.
//   - It never runs forever: it stops at the first "done", at "none" (nothing left
//     to wait for), or after MAX_WAIT_MS, whichever comes first.
//
// THE PAGE WORKS WITHOUT IT. With the script blocked, this screen simply stays as
// it is; the tab the plaque opened says "Activation complete" on its own.
(function () {
  'use strict';

  var root = document.querySelector('[data-activation-wait]');
  if (!root || !window.fetch) return;

  var url = root.getAttribute('data-status-url');
  var pending = root.querySelector('[data-wait-pending]');
  var done = root.querySelector('[data-wait-done]');
  if (!url || !pending || !done) return;

  // Three seconds feels immediate to someone switching back from the other tab,
  // and the server meters this endpoint on its own budget (statusLimit).
  var INTERVAL_MS = 3000;
  // Ten minutes: long enough to walk to the door, short enough that a forgotten
  // tab stops asking. Coming back to the tab asks once more (below).
  var MAX_WAIT_MS = 10 * 60 * 1000;

  var started = Date.now();
  var timer = null;
  var finished = false;

  function stop() {
    finished = true;
    if (timer !== null) {
      window.clearTimeout(timer);
      timer = null;
    }
  }

  function showDone() {
    stop();
    pending.hidden = true;
    done.hidden = false;
    document.title = 'Activation complete — Taptime';
    // Move focus so a screen reader announces the new state rather than leaving
    // the user on a block that no longer exists.
    if (typeof done.focus === 'function') done.focus();
  }

  function schedule() {
    if (finished) return;
    if (Date.now() - started > MAX_WAIT_MS) {
      stop();
      return;
    }
    timer = window.setTimeout(check, INTERVAL_MS);
  }

  function check() {
    timer = null;
    if (finished) return;
    window
      .fetch(url, { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } })
      .then(function (res) {
        return res.json();
      })
      .then(function (body) {
        if (body && body.state === 'done') {
          showDone();
        } else if (body && body.state === 'none') {
          stop();
        } else {
          schedule();
        }
      })
      .catch(function () {
        // A dropped request (a train tunnel, a sleeping radio) is not an answer.
        schedule();
      });
  }

  // Switching back to this tab is exactly when the answer is most likely to have
  // changed, so ask straight away instead of waiting out the interval — and give
  // a tab that had timed out one more look.
  document.addEventListener('visibilitychange', function () {
    if (document.visibilityState !== 'visible' || done.hidden === false) return;
    if (finished) {
      finished = false;
      started = Date.now();
    }
    if (timer !== null) {
      window.clearTimeout(timer);
      timer = null;
    }
    check();
  });

  schedule();
})();
