// The operator enrollment screen's one script (M10 OP-8; ADR 0020 §3, §6).
//
// WHAT IT IS FOR. The enrollment link carries the one-time token in its FRAGMENT --
// /operator/enroll?id=<account>#<token> -- which a browser does not put in a request, so
// the token is not in the request line an ingress or this product's access record logs
// (ADR 0020 §6: the token does not travel in a query string). This file moves the token
// from the fragment into the form's token field, takes the fragment out of the address
// bar, and tucks the field away.
//
// THE POLICY THAT PERMITS IT: the enrollment page's Content-Security-Policy is
// script-src <operator origin>/static/js/operator/enroll.js (internal/handler/operator,
// enrollCSP); no 'unsafe-inline', no 'unsafe-eval'.
//
// WHAT IT TOUCHES: the token field's value, its wrapper's [hidden] attribute, and the
// current history entry's URL (replaceState, which keeps the path and the query and
// drops the fragment). The code below makes no request, uses no storage, sets no cookie
// and does not read the password or code fields -- read by a reviewer; the browser
// behaviour was measured by hand (m10-platform.md, L15), and the tests pin the code's
// text (TestEnrollScript_IsTheReviewedBody, TestEnrollScript_TouchesTheFragmentAndNothingElse).
//
// WITHOUT IT the field stays on screen and the person pastes the part of the link
// after the # sign: the form posts the same fields either way.
(function () {
  'use strict';

  var field = document.getElementById('enroll-token');
  var wrap = document.getElementById('enroll-token-wrap');
  if (!field || !wrap) return;

  // A token is the shape the server mints: 43 characters of unpadded base64url
  // (internal/operatorauth, NewEnrollmentToken). A fragment of another shape is not
  // copied into the field.
  var fragment = window.location.hash.slice(1);
  if (/^[A-Za-z0-9_-]{43}$/.test(fragment)) {
    field.value = fragment;
  }
  if (window.location.hash) {
    window.history.replaceState(null, '', window.location.pathname + window.location.search);
  }
  if (field.value) {
    wrap.setAttribute('hidden', '');
  }
})();
