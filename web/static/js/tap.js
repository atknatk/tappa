// The tap screen's only script. It does ONE thing: when the button is pressed,
// ask the browser for a single position fix, put it in three hidden fields, and
// submit the form.
//
// 🔴 CLAUDE.md §4.2 IS THE SPEC OF THIS FILE: location is read ONLY at the
// moment of a tap, and none of the three shapes of continuous location — from
// the background, by subscription, or by boundary monitoring — may exist in this
// product. So:
//
//   - getCurrentPosition, ONE call, started by a click and by nothing else.
//     There is no continuous-position subscription here and there must never be
//     one: the browser API that keeps reporting a position is what turns "where
//     are you now" into "where have you been", and it is precisely what the red
//     line forbids. Same for anything that watches a boundary or reports from
//     the background. scripts/redline-check.sh R2 scans the SOURCE TREE for
//     those three API names and FAILS the build on a match, which is why they
//     do not appear here even as prose — a scanner that has to be argued with
//     is a scanner that eventually gets loosened. (Its scope is source, not
//     documentation: CLAUDE.md §4.2 and docs/adr/0005 name the same APIs in
//     order to forbid them, and that is correct.)
//   - Nothing is stored. No localStorage, no cookie, no variable that outlives
//     the submit. The coordinates exist in two form fields for the length of one
//     POST.
//   - Nothing is sent anywhere else. No fetch, no beacon, no third party. The
//     page has no other origin to talk to.
//
// GPS IS OPTIONAL EVIDENCE AND THE FLOW NEVER DEPENDS ON IT (§5 rows 6-7: an IP
// match alone is enough for `ok`, and neither one is a `flag` — a recorded
// decision, not a refusal). So every failure path here does the same thing:
// submit without coordinates. A person who denies the permission still clocks
// in; the worst outcome is that their manager confirms it.
//
// IT ALSO WORKS WITH THIS FILE ABSENT. The form is a plain POST with empty
// coordinate fields, so a browser with JavaScript disabled — or a work phone
// that blocks it — submits successfully and lands on the IP path.
(function () {
  'use strict';

  var form = document.querySelector('[data-tap-form]');
  if (!form) return;

  var button = form.querySelector('[data-tap-button]');
  var lat = form.querySelector('[data-tap-lat]');
  var lng = form.querySelector('[data-tap-lng]');
  var acc = form.querySelector('[data-tap-acc]');
  if (!button || !lat || !lng || !acc) return;

  // Guards the whole interaction. Once a submit is really under way the button
  // is disabled and this stays true, so a double tap on a laggy phone cannot
  // produce two POSTs (the server would answer the second with a recorded
  // `ignored` under the 60-second debounce, but a screen that visibly does
  // nothing twice is a screen people press harder).
  var submitting = false;

  // How long to wait for a fix before giving up and submitting anyway. Eight
  // seconds is long enough for a cold GPS lock indoors on a phone that has just
  // been unlocked, and short enough that nobody standing at a door with a queue
  // behind them thinks the page is broken. Refusing to wait longer is the whole
  // point: a tap that goes through without coordinates is a tap; a tap that
  // never goes through is a missing hour.
  var FIX_TIMEOUT_MS = 8000;

  // markPending is called the INSTANT a press is accepted, not when the request
  // finally goes out. Between those two moments there is a geolocation fix being
  // waited for, and a button that still looks pressable is a button that gets
  // pressed again by someone with wet hands and a queue behind them.
  function markPending() {
    if (button) {
      button.disabled = true;
      button.textContent = 'Tapping…';
    }
  }

  function send() {
    markPending();
    form.submit();
  }

  form.addEventListener('submit', function (event) {
    if (submitting) {
      // 🔴 A SECOND PRESS WHILE THE FIRST IS STILL WAITING FOR A FIX, and this
      // branch MUST cancel it. An audit measured the earlier version, which
      // returned here WITHOUT preventDefault: the browser then carried out the
      // native submission immediately — with the coordinate fields still empty,
      // because the fix had not arrived — and the first press's callback landed
      // on a page that was already navigating away. Nothing was lost from the
      // record (§4.6 is safe: the tap is still posted), but the GPS EVIDENCE
      // was, so a tap that would have scored `ok` on the venue's coordinates
      // arrives with neither IP nor GPS and becomes a `flag` for a manager to
      // clear by hand. On a wet-or-gloved-fingers screen a double press is not
      // an edge case, it is the expected input.
      event.preventDefault();
      return;
    }
    submitting = true;

    if (!navigator.geolocation) {
      // No geolocation at all (an old browser, an embedded webview). Let THIS
      // submit proceed natively — the fields stay empty, which §5 rows 6-7
      // already treat as a legitimate state. The button is still marked so a
      // second press has nothing to hit while the navigation starts.
      markPending();
      return;
    }

    // From here we take over the submit so the request carries the fix.
    event.preventDefault();
    markPending();

    var done = false;
    function finish() {
      if (done) return;
      done = true;
      send();
    }

    navigator.geolocation.getCurrentPosition(
      function (position) {
        // Written straight into the form and nowhere else. Precision is left
        // exactly as the browser gave it: the server compares it against the
        // venue's radius and stores a DISTANCE, not a coordinate (§4.7 keeps
        // full coordinates out of logs), and rounding here would only make an
        // honest tap look further away than it is.
        lat.value = position.coords.latitude;
        lng.value = position.coords.longitude;
        acc.value = position.coords.accuracy;
        finish();
      },
      function () {
        // Denied, unavailable, or timed out. Deliberately indistinguishable to
        // this code: all three mean "no coordinates", and none of them is a
        // reason to stop somebody clocking in. The error object is NOT logged —
        // there is no console line here, because the one thing it could carry
        // is a position.
        finish();
      },
      {
        enableHighAccuracy: true,
        timeout: FIX_TIMEOUT_MS,
        // Refuse a cached fix outright. A remembered position is exactly the
        // "where you were" signal §4.2 exists to keep out of this product, and
        // as evidence of presence it is worthless: it would let a phone that has
        // not moved since this morning vouch for someone who has.
        maximumAge: 0,
      }
    );

    // Belt and braces: if the callback never fires at all (some webviews go
    // quiet after a permission prompt), submit anyway a beat after the timeout.
    window.setTimeout(finish, FIX_TIMEOUT_MS + 500);
  });
})();
