// The app consent page's one script: the "Allow" button's arming delay (issue #37, ADR 0047's
// "Uygulama notları" -- double-clickjacking).
//
// 🔴 THE THREAT: a page on another site gets the person to double-click, and between the two clicks
// swaps itself for this consent page (a window opened by the first click closes, or the opener is
// navigated). The second click then lands on "Allow" without the person ever having read the page.
// The defence is time: "Allow" cannot be pressed until the page has been VISIBLE AND FOCUSED for
// ARM_MS. Losing visibility or focus disarms it again; regaining them starts the wait over.
//
// 🔴 IT IS A FILE, NOT INLINE. internal/handler/oauthconsent.go answers the consent page -- and only
// it -- with the panel's policy plus script-src 'self'; there is no 'unsafe-inline' and no
// 'unsafe-eval'. WHAT IT TOUCHES: the disabled state of one button. No fetch, no storage, no cookie,
// no location read, nothing of the form's values.
//
// THE BUTTON SHIPS disabled AND THIS FILE IS WHAT ENABLES IT. A browser that runs no script gets the
// page's <noscript> twin of the form instead (enabled, without the delay -- a named limit); a browser
// that runs scripts but fails this file sees a disabled "Allow" and can still refuse.
(function () {
  'use strict';

  var ARM_MS = 600;
  var allow = document.querySelector('[data-oauth-allow]');
  if (!allow) return;
  var timer = null;

  function disarm() {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
    allow.disabled = true;
  }

  function arm() {
    disarm();
    if (document.visibilityState !== 'visible') return;
    timer = setTimeout(function () {
      timer = null;
      allow.disabled = false;
    }, ARM_MS);
  }

  arm();
  document.addEventListener('visibilitychange', arm);
  window.addEventListener('focus', arm);
  window.addEventListener('blur', disarm);
  window.addEventListener('pageshow', arm);
})();
