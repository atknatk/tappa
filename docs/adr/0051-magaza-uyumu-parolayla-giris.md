# ADR 0051 — Mağaza uyumu: çalışanın e-posta + parolayla girişi, uygulamadan işletme kaydı, işletme hesabını silme

- **Durum:** Önerildi (2026-11-25; aynı gün üç denetim turunun NO-GO bulgularıyla üç kez yeniden yazıldı — kimlikler köşeli parantezde:
  *[B…]* fizibilite/güvenlik, *[A…]* sıfır hafızalı yürüyüş; üçüncü tur *[B3-…]*, *[A3-N…]*) — kabul M16-50'de, kullanıcının. Açık sorular
  **Q55 … Q59**; önerilmeyen bir cevap planı yeniden yazdırır ve yeniden denetletir (§7). Kabul edilince **ADR 0049 §2'nin link mekanizmasının
  yerini alır**.
- **Tarih:** 2026-11-25 · **Dal:** `m16-native-ui` (PR #49, `main` dışı; yalnız belge) · **Plan:** [m16-mobil.md](../plan/m16-mobil.md) → *Faz H*
- **Karar veren:** kullanıcı, 2026-11-25, AskUserQuestion — §2.1'in maddeleri (**1–7**, **K-A, K-B, K-C2, K-D**) sabittir; **K-C2, K-C'nin yerini
  aldı** (demo işletme operatörden değil, `/signup`'tan). Geri kalan öneridir.
- **İlgili:** ADR 0002 md. 7 · 0005 · 0010 · 0021 · 0022 · 0025 · 0026 · 0027 · 0031 (§5.6, §5.9, §6) · 0039 · 0045 · 0048 · **0049** · CLAUDE.md §4,
  §5, §6, §7, §9 · Q13, Q23, Q28, Q40, Q44–Q49.
- **Atıf kuralı:** `file:line` atıfları `m16-native-ui` `f9b972a5`'e göredir (kod `828ce9b1`'den beri değişmedi). **Okundu** = dosyadan okundu.
  **Ölçülmedi** = ilgili kart ölçer. Apple ve Google sayfaları 2026-11-25'te okundu (§1.17).

---

## 1. Bağlam — bugün (okundu)

1. **ADR 0049 uygulanmadı** (`git grep -n "signin/request" -- '*.go'` → 0); `/app/v1` yolları `internal/handler/app.go:207-224`; hata kodları
   `app.go:270-300` (`rate_limited`, `not_found`, …).
2. **İlk ekran davet kodu** (iOS `ActivationCopy.swift:20`; Android `res/values/strings.xml:21`); lookup → bind → complete (`appactivate.go:15-28`);
   lookup plaketsiz `employeeName`, `businessName`, `locationName`, `wifiSsid` döndürür (`:48-59`).
3. **Statik QR aktive etmez** (`appactivate.go:249`, `:324-327`; `activate.go:978`); NFC'siz yollar M16-23 (`approval.go:18-32`, `:317`).
4. **Davet:** `status = 'active'`'i yalnız `ConsumeInviteAndActivate` yazar (`invites.sql:72-97`); `IssueAndDeliver` basımda bütün bekleyenleri
   iptal eder (`manager.go:237-254`, `:300-320`); `consume` (`:873`) kardeşleri iptal etmez; `IssueParams` yalnız `TenantID`, `EmployeeID`, `TTL`,
   `OnlyWithoutAddress` (`:203-216`); `CreateInvite` köken yazmaz (`invites.sql:66-71`); sayım her satırı sayar (`:410-425`), kilit ve sayım yalnız
   e-posta kanalında (`email.go:394-422`); e-posta işletme adını yalnız VIES doğrulanmışsa yazar (`email.go:148-155`, `inviteemail.go:48-53`).
5. **Parola işi:** bcrypt yalnız `adminauth/password.go` ve `operatorauth`'ta; cost 12, `Hash`/`Compare`/`CompareDummy` (`password.go:180`, `:324`,
   `:346`), 72 bayt, en kısa 8; `WorkGate` yalnız `WithWorkGate` (`workgate.go:179-273`, `:286-292`), wiring testi (`workgatewiring_test.go:21`);
   panel bütçeleri (`adminratelimit.go:655-681`, `:762-803`); `TryCharge` dersi (`signupconfirm.go:586-594`); anahtar türetme etiketleri
   `"tappa/<amaç>/v1/key-derivation"` biçimindedir (`admininvite.go:64`, `signupclaim.go:68`; türetme `admininvite.go:136-142`).
6. **Oturumsuz kimlik kanıtı hazır değil** (`identity.go:129-136`; `signupconfirm.go:583-597`; `identityops.go:196-206`). Bir admin'in bütün
   oturumlarını kapatan sorgu vardır: `RevokeAdminSessionsForAdmin` (`db/queries/admins.sql:203`).
7. **`employees`:** e-posta tekilliği `00037:96-105`; adres `SetEmployeeEmail` ve beş başka `UPDATE` ile değişebilir (`employees.sql:764`, `:845`,
   `:1007-1030`, `:1068`, `:1180`, `:1382`); kişi sütunlarının CHECK'leri — `external_ref` biçimi `^[A-Za-z0-9][A-Za-z0-9._~-]{0,63}$`, `employee_code`,
   `first_name`, `last_name` (1–120, kırpılmış, kontrol karakteri yok) `00037:63-79`; **`location_id IS NOT NULL OR external_ref IS NOT NULL`**
   (`:89-90`); `(tenant_id, external_ref)` tekil (`:110-112`); `full_name text NOT NULL` (`00003:41`). Sıralama dersi `00017:30-60`, `:85-95`.
   `tappa_resolver`'ın yetkileri: `employee_invites` (`00009:242-243`, `00012:87`), `admin_users (id, tenant_id, email, password_hash, status)`
   (`00011:212-213`) + `created_at`, `identity_id`, `unlinked_at`, `role` (`00017:346`, `00071:129`, `00081:94`), `tenants.id` (`00051:155`).
8. **`tenants`:** `vat_number NOT NULL UNIQUE` (`00001:15`) — **K-C2 bu yüzden grubun başka bir şirketinin gerçek numarasını kullanır**;
   `tappa_app`'ın INSERT'i sütun düzeyi (`00016:85-89`). ADR 0021'in *"ASLA"* sütunu `tappa_opdefiner`'a `tenants`'ta askı / doğrulama / API sütunları
   dışında UPDATE ve hiç INSERT/DELETE vermez (`docs/adr/0021-*.md:162-170`; `00063:296-307`; `privcatalog_test.go:283`; `holdingops_test.go:743`).
   Askı nedenleri (`00063:354`), `op_suspend_tenant(p_session, p_tenant_id, p_reason)` (`:943`); `operator_audit_log_kind_check`'in son tanımı
   `00107:236-245`.
9. **Kimlik yüzeyi:** hiçbir operatör rolü `identity_*` / `resolve_identity_*` çalıştıramaz — CLAUDE.md §4.5'in üçüncü istisnası, pinler
   `internal/db/identitycatalog_test.go:408-415` (`TestIdentityFunctions_NoOperatorRoleMayExecuteThem`), `internal/db/adminidentity_test.go:179`, `:283`.
   **`tappa_app`'ın bugün çalıştırabildiği 30 `identity_*` fonksiyonu** (ölçüldü: `git grep "GRANT EXECUTE ON FUNCTION public.identity_" … TO tappa_app`):
   `accept_invite`, `accept_invite_new`, `change_email`, `claim_signup`, `confirm_window`, `consume_reset`, `consume_row_reset`, `create_for_signup`,
   `keep_all`, `link_by_email`, `link_legacy`, `memberships`, `open_reset`, `open_row_session`, `open_session`, `open_signup_claim`,
   `password_for_session`, `record_failed_login`, `relink_refused`, `request_email_change`, `request_verification`, `revert_email_change`,
   **`revoke_sessions`**, `set_password`, `sweep_events`, `sweep_signup_claims`, `switch_session`, `unlink_backfilled`, `verify_email`,
   `withdraw_reset`. **Bir kimliği bir işletmeden silen / anonimleştiren fonksiyon yoktur.** `admin_identities.email NOT NULL` (`00069:168`);
   bcrypt CHECK'ini geçen rastgele özet emsali `00102:565` (`crypt(…, gen_salt('bf', 12))`). Kimlik yan tabloları: `admin_identity_resets`,
   `admin_identity_verifications`, `admin_identity_events` (`00072:167`, `:193`, `:221`). **`admin_signup_claims`'in `tenant_id`'si yoktur ve kullanılmış
   satırları süpürülür** (`00078:123`; `signupconfirm.go:311` `Sweep`).
10. **Askının kesmedikleri** (ölçüldü, `internal/handler/api/suspension.go:1-30`): `/v1` GET'leri açık, webhook gönderimi ve davet kuyruğu askıya bakmaz,
    anahtar iptali ve uç kapatma panelde açık (K-26). OAuth onayı askıda reddedilir (`oauthconsent.go:282`); verilmiş token'ların askıda çalışıp
    çalışmadığı **ölçülmedi**. Tablolar: `api_keys` (`00051:74`), `oauth_grants` / `oauth_tokens` (`00104:189`, `:359`), `webhook_endpoints` (`00048:186`).
11. **Ekleme-yalnız tablolar** (her rol için UPDATE/DELETE yasak): `transactions`, `transaction_reviews` (`00005:305`), `punch_claims` (`00068:134`),
    **`punch_claim_decisions`** (`00068:261`; `note` sütunu `:235`), `site_punches` (`employee_name` `00046:130`; tetikleyici `:183`), `punch_events`
    (`00048:151-156`). Yedekler 30 günde düşer (`scripts/pg-backup-ship.sh:40`).
12. **Kayıt sihirbazı:** KDV zorunlu ve biçimli (`internal/domain/signup/signup.go:319-351`); onay e-postayla (`signupconfirm.go:23-44`); `SignupDone`
    düğmesi (`:562-571`; `signup.templ:697`); sandbox'ta onay linki ekranda (`signup.templ:765`, `ScreenLink`).
13. **Kabuk bağlantıları:** `publicchrome.templ:170`, `:180`, `:186`, `:197-205`, `:213`, `:277` (*Built by*), `:281`, `:292`; `signup.templ:290`, `:840`;
    yasal gövde `legal.templ:158-163`; `privacy-draft.md:68`.
14. **App Link** yalnız `/t`, `/q` (`applinks.go:70`). **Ingress:** erişim log'suz `tappa-panel` (`40-ingress.yaml:226-259`), pin
    `panelingress_test.go:45`; **her `main` deploy'u Ingress'i uygular** (`deploy.yml:934-935`). `Referrer-Policy: no-referrer` emsali
    `adminreset.go:909`, `accountemail.go:487`. İptal edilmiş oturumun tap dalı `tap.go:508-512`.
15. **`scoped-access.sql`** (ADR 0048, `origin/ops/tappa-scoped-access`): tablo düzeyi `GRANT SELECT ON ALL TABLES` (`:146-148`), yalnız `tags` sütun
    listesiyle; **`ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner … GRANT SELECT ON TABLES TO tappa_readonly, tappa_readwrite`** (`:274-275`) — yeni her tablo
    migration koştuğu an **iki BYPASSRLS rolüne** okunur olur *[B3-N11]*. Roller dev'de yoktur (`scripts/db-init/01-roles.sql` → 0 eşleşme), ops dalında
    vardır (`:262` civarı). Bugün `admin_users.password_hash` de bu yolla okunurdur.
16. **SigNoz kuralları** `deploy/observability/signoz-alert-rules.json` (dokuz kural, *"tappa 1"* … *"tappa 9"*).
17. **Mağaza (okundu 2026-11-25):** Apple *"Offering account deletion in your app"* (tam hesap kaydını sil; yalnız devre dışı bırakmak yetmez; e-posta /
    destek akışı isteme; süreyi söyle, tamamlanınca onay ver; kimlik doğrulama adımı eklenebilir; saklanacak veriyi söyle). Google
    *answer/13327111* (web bağlantısı çalışmalı; hesaba bağlı veriyi sil; yasal nedenle saklananı açıkça söyle) ve *answer/10787469* (*"Provide information
    for Google Play's Data safety section"* — formda *"Does your app provide a way for users to request deletion of their data?"*). Öteki maddeler (2.1(a),
    3.1.1, 3.1.3(c), 3.2, 4.8) ana oturumun okuması.

## 2. Karar

### 2.1 Kullanıcı kararları (2026-11-25) — sabit

1. İlk ekran *Sign in* + *Create a business account*. 2. Çalışan girişi e-posta + parola. 3. Bağ ilk plaket / QR dokunuşunda. 4. Uygulama içi kayıt
uygulama modunda biter. 5. İşletme silme owner'ın uygulama içi talebi. 6. Demo *"Taptime Demo"*; inceleyici demo çalışanın kimliğini yalnız konsollarda alır.
7. Önce Play iç testi; üretim PR #49'u, hukukçu onaylı metni, `TAPPA_APP_ANDROID_SIGNER_SHA256`'yı bekler.
8. **K-A** — silme 30 günde; askı + kişisel verinin anonimleştirilmesi; kayıtlar yasal saklamada; onay e-postası; hukukçu + Q13 kapı.
9. **K-B** — *Built by* uygulama modunda kalır.
10. **K-C2 (K-C'nin yerine)** — demo işletme **normal web `/signup`'tan**, kullanıcının **grup şirketlerinden birinin gerçek AB KDV numarasıyla** açılır
    (KF ya da KM değil — `vat_number` tekil). Numarayı kullanıcı yazar, depoya asla. **Şema değişikliği ve operatör yolu yok.**
11. **K-D** — holding sitelerinin ekleme-yalnız kayıtlarındaki adlar kalır; sayfa ve onay e-postası söyler; yedekler 30 günde düşer; hukukçuya *"adı olmayan
    kayıt hâlâ delil midir?"*.

### 2.2 Kimlik bilgisi: çalışan satırı başına, adrese bağlı parola özeti

- **`employee_passwords`** (`tenant_id`, `employee_id`, `digest`, `set_email citext NOT NULL`, `set_at`); parola yalnız `set_email = employees.email` iken
  geçerli — adres hangi yoldan değişirse değişsin parola etkisiz *[B2-H2]*. `tappa_app`'a `SELECT, INSERT, UPDATE (digest, set_email, set_at), DELETE`.
- **`employee_password_links`** (`…, token_hash, link_email citext NOT NULL, created_at, expires_at, used_at, revoked_at`); **tek canlı link:** kısmi `UNIQUE
  (tenant_id, employee_id) WHERE used_at IS NULL AND revoked_at IS NULL` — süresi dolmuş ama iptal edilmemiş link indeksi tutardı, bu yüzden **yeni basım
  önce kişinin süresi dolmuş canlı linklerini `revoked_at = now()` ile kapatır** (aynı transaction) *[A3-N22]*; `tappa_app`'a `SELECT, INSERT, UPDATE
  (used_at, revoked_at), DELETE`.
- **Gizli sütunlu tablolar BYPASSRLS rollerinden geri alınır** *[B3-N11]*: iki tabloyu yaratan migration, rol varlığına bağlı bir `DO` bloğuyla
  (`IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'tappa_readonly')` …) `REVOKE ALL ON employee_passwords, employee_password_links FROM tappa_readonly,
  tappa_readwrite` yapar (dev'de roller yok, blok bir şey yapmaz); `scoped-access.sql`'e iki tablo için kalıcı istisna (yeniden koşu `GRANT SELECT ON ALL
  TABLES`'ı geri getirmesin diye) bir kapıdır. **`admin_users.password_hash`'in bugün `tappa_readonly`'ce okunur olması** ADR 0048'in kabul ettiği bir
  durumdur (BYPASSRLS okuyucu insan erişimi; bcrypt cost 12) — bu ADR onu değiştirmez, M16-50'nin notunda **adlandırılmış bedel** olarak yazılır.
- **ADR 0021 §1'in sır sütunları listesine** `employee_passwords.digest`, `employee_password_links.token_hash` eklenir; `privCatalogAllowed` (`privcatalog_test.go:283`)
  iki tablonun `tappa_app` ve `tappa_resolver` satırlarını alır *[A3-N25]*.
- Kod `internal/adminauth`'ta; ret listesi (lisansı uyumlu ≤ 10 000 giriş ya da ≤ 300 elle kalıp); telefonda parola yok.

### 2.3 İlk parola ve sıfırlama

1. `POST /app/v1/password/request` gövde `{"email": string}` → her durumda `202`, boş gövde; kaynak kovası `429 {"error":"rate_limited"}` + `Retry-After`;
   `[250, 300] ms` bandı *[A3-N18]*.
2. Kim: (a) aktif ya da (b) işverenin canlı davetini taşıyan davetli (Q59); env + owner ayarı; askıda değil; `app_signin_revoked_at IS NULL`.
3. Çözümleyici — bütün süzgeçler (yönetici adresi dışlaması dahil) `LIMIT`'ten önce, sıra `t.created_at, e.created_at, e.id`, `LIMIT 3`.
4. Link: 256 bit token; HMAC anahtarı `SessionHMACKey`'den **`"tappa/staff-password-link/v1/key-derivation"`** etiketiyle türetilir *[A3-low]*; ömür 1 sa;
   `link_email` eşleşmesi; çıkış kutusu; `LockTenantForStaffPasswordLimits`. **Token CLAUDE.md §7'nin asla-loglanmaz listesindedir** (davet kodu ailesi).
5. Sayfa `GET/POST /staff/password`: GET token'ı çereze alır, token'sız adrese 303, `Referrer-Policy: no-referrer` (`adminreset.go:909`); POST
   `LockTenantForInviteLimits` altında özet + linkler + `CancelPendingPasswordInvitesForEmployee`.
6. Bildirimler `personNotices`; bütün yeni e-postalar `TenantVerified` kuralıyla.

### 2.4 Giriş

1. `POST /app/v1/signin` gövde `{"email": string, "password": string}`. Cevaplar *[A3-N18]*: `200 {"workplaces":[{"code": string (43 karakter base64url —
   `internal/invite/code.go:55-57`), "businessName": string}]}` (1–3 öğe) · `401 {"error":"signin_failed"}` · `429 {"error":"rate_limited"}` + `Retry-After` ·
   `503 {"error":"busy"}` + `Retry-After: 60` · env boşken `404 {"error":"not_found"}` (`app.go:285`). Yeni kodlar `signin_failed`, `busy`
   `appErrorStatus` haritasına girer.
2. Aday çözümleyicisi — süzgeçler + `set_email` `JOIN`'i `LIMIT`'ten önce; doygunluk kehaneti kapalı (sayıyı yalnız doğru parola görür).
3. Basım `IssueAndDeliver` — **M16-52'nin işi:** `IssueParams.Origin`, `IssueParams.Gate`, `CreateInvite`'e `origin`; basımdaki iptal kuralı değişmez; ömür 24 sa.
4. **Tüketim kardeşleri iptal eder**; işveren kökenli tüketim `app_signin_revoked_at`'i temizler; **`app_signin_revoked_at` doluyken `origin='password'` bir
   davet tüketilemez** (`consume` reddeder — silme talebiyle yarışan bir koda karşı) *[B3-N-M2]*.
5. Parola davetinin lookup'ı `locationName` / `wifiSsid` döndürmez.
6. Owner'a saatlik özet, kişiye bildirim, onay listesi uyarısı, T59 (a), kurtarma yolu, kilit yok.

### 2.5 Bütçeler

Hepsi karşılaştırmadan önce `TryCharge` ile: giriş — kaynak başarısız 10 / 10 dk, karşılaştırmaya ulaşan 30 / 10 dk, hesap izi 10 / 10 dk (kapatmaz); parola daveti
kişi 5 / sa; link isteği kaynak 5 / 10 dk + 20 / gün, süreç 100 / sa, işletme 10 / sa + 30 / gün, kişi 1 / sa + 3 / gün; parola sayfası link başına 10 / 10 dk.
`CountRecentInvites` `origin = 'employer'`'a daralır. Küresel tavan (hesap): ≈ 0,88 tahmin/sn.

### 2.6 Uygulama modu

- **Taşıma** *[A3-N13]*: `GET /signup?client=app` bayrağı sihirbazın **imzalı durum çerezine** (`signupstate.go`, yol `/signup`) yazılır; sonraki GET ve POST'lar onu
  oradan okur — form alanı ya da sorgu dizgesi taşımaz; son POST'ta taslağın JSON'una (`signupClaimer.Open(…, draft []byte)`) geçer. **Onay linki e-postadan
  sistem tarayıcısında açılır** (uygulamanın dışında); orada `SignupDone`'ın uygulama modu metni taslaktan gelir ama 3.1.1 için gerekmez — uygulamanın içinde
  değildir; uygulamanın içinden fiyata ya da panele bağlantı yoktur.
- **Kaybolan:** `publicchrome.templ:170`, `:180`, `:186`, `:197-205`, `:213`, `:281`; `signup.templ:290`, `:840`, **`:697`** (*Sign in to your dashboard*).
  **Kalan:** `:277` (K-B), `:292` + yasal iç bağlantılar (`?client=app`), dış bağlantılar, `mailto:`, çapalar, **`signup.templ:765`** (onay linki — yalnız
  e-posta gönderimi kapalı sandbox/dev kurulumunda çizilir; sihirbazın kendi akışıdır). `/account-deletion?client=app` aynı testte.

### 2.7 İnceleyici bağı — Q55 (öneri (b): işaretli işletmede statik QR)

### 2.8 Açılış — Q56

Öneri (b): env `TAPPA_STAFF_PASSWORD_TENANTS` + owner ayarı (eski kapalı, yeni açık; `signup.Provision` aynı transaction'da `UPDATE`). Birleştirmede env'den
bağımsız canlıya çıkanlar: migration'lar, Ingress yolları, `CountRecentInvites` daralması, tüketimdeki kardeş iptali (her işletmede), panelin owner ayarı,
`/account-deletion`'ın çalışan bölümü.

### 2.9 İşletme hesabını silme — K-A, K-D; giriş yeri Q57

1. **Çalışanın kendi talebi — M16-52 yapar** *[B2-H1, B3-N-M2, A3-N12]*: `RequestDeletion` (`staffdeletion.go`) aynı transaction'da kişinin `employee_passwords`
   ve `employee_password_links` satırlarını siler, `app_signin_revoked_at`'i yazar ve **`CancelPendingPasswordInvitesForEmployee`**'yi çağırır.
2. **Sayfa** `/account-deletion` — iki bölüm (çalışan yolu + owner formu); owner formu env **`TAPPA_BUSINESS_DELETION`** arkasında: **değer sözdizimi** —
   boş ya da tanımsız = kapalı; `on` = açık; başka her değer açılışı reddeder (`TAPPA_INVITE_DELIVERY`'nin kapalı küme kuralı) *[A3-N19]*. Bayrak, M16-61
   üretimde çalışana dek açılmaz; mağaza üretim gönderimi bayrağı bekler; Faz H PR'ı Q13 / hukukçu beklerken birleşebilir.
3. **Kanıt** `adminauth.Manager.ProveOwnership` (yeni; oturumsuz); çok adımlı durum HMAC'li 10 dakikalık çerez + senkronizasyon token'ı + Origin.
4. **Talep yeni, tenant kapsamlı bir tabloda** *[B3, A3-N7]*: **`business_deletion_requests`** — `id`, `tenant_id`, `requested_by` (owner'ın `admin_users.id`'si),
   `requested_at`, `approved_at`, `completed_at`, `cancelled_at`; §6 beşlisi; **kişi başına değil işletme başına tek açık talep** (kısmi `UNIQUE (tenant_id) WHERE
   completed_at IS NULL AND cancelled_at IS NULL`); **tek yönlü tetikleyici** (`approved_at`, `completed_at`, `cancelled_at` yalnız `NULL` → değer; `completed_at`
   yalnız `approved_at` doluyken; `cancelled_at` yalnız `completed_at` boşken). `tappa_app`'a `SELECT, INSERT, UPDATE (completed_at)`; **`tappa_opdefiner`'a
   yalnız bu tabloda `SELECT` ve `UPDATE (approved_at, cancelled_at)`** — `tenants`'ta yeni UPDATE sütunu **açılmaz**, ADR 0021'in *"ASLA"* hücresi değişmez.
   `tenants`'a sütun eklenmez.
5. **Talep transaction'ı (tappa_app):** satır + `tenant.deletion_requested` audit + **`RevokeAdminSessionsForAdmin`** (`admins.sql:203` — var olan sorgu; yeni bir
   `RevokeMembershipSessions` **yok**) *[A3-N20]* + bağlı kimliğin bu işletmedeki oturumları için var olan **`identity_revoke_sessions`**. Sonra isteyene ve
   öteki owner'lara e-posta.
6. **Operatör:** `op_read_deletion_requests(p_session, p_ticket)` (yeni; en eski önce, kalan gün), `op_approve_business_deletion(p_session, p_request_id)` (yeni;
   `approved_at`), `op_cancel_business_deletion(p_session, p_request_id)` (yeni; `cancelled_at`); yeni `operator_audit_log` türleri **`business_deletion_read`,
   `business_deletion_approved`, `business_deletion_cancelled`** — `operator_audit_log_kind_check` `00107:236-245`'in deseniyle (`DROP` + `ADD … NOT VALID` +
   `VALIDATE`) genişler *[A3-N21]*. Onaydan hemen sonra operatör yüzeyi **var olan `op_suspend_tenant(…, 'customer_request')`**'i çağırır.
7. **Anonimleştirme uygulama tarafında** *[B3]*: operatör handler'ı onay commit edince tüketici tarafı bir arayüzü (`BusinessEraser`, `internal/handler/operator`'da
   tanımlı; uygulaması `internal/domain/tenant`'ta; `cmd/tappa` bağlar) çağırır; silici **`tappa_app` havuzunda, `SET LOCAL app.tenant_id` ile** çalışır.
   Süreç çökerse açılışta bir süpürücü bekleyenleri bulur: **`resolve_pending_business_erasures()`** (yeni, `tappa_resolver` sahipli, yalnız `tenant_id` döndürür;
   `business_deletion_requests (tenant_id, approved_at, completed_at, cancelled_at)` sütun yetkisi). **Hiçbir operatör rolü kimlik fonksiyonu ya da tablosuna
   dokunmaz;** `identitycatalog_test.go:408-415`, `adminidentity_test.go:179`, `:283` değişmez.
8. **Kimlik tarafı — tek yeni fonksiyon, ADR 0031 kapalı listesine eki** (M16-50'nin kullanıcı kapısında adıyla): **`identity_erase_member(p_tenant_id uuid,
   p_admin_user_id uuid)`** — sahibi `tappa_identitydefiner`, `EXECUTE` **yalnız `tappa_app`** (öteki 30'un modeli): üyeliği kapatır, kişinin bu işletmedeki
   oturumlarını iptal eder; kimliğin **başka aktif üyeliği yoksa** e-postayı `deleted-<identity id>@deleted.invalid`, özeti `00102:565`'in rastgele bcrypt
   özetiyle değiştirir, canlı `admin_identity_resets` / `admin_identity_verifications` satırlarını siler. `admin_identity_events` ekleme-yalnızsa kalır (kart
   ölçer). Var olan `identity_revoke_sessions` oturumlar için yeterliyse fonksiyon yalnız anonimleştirmeyi yapar.
9. **Envanter — sütun başına değer** *[B3, A3-N8–N10, B3-N-L1]*:

   | Tablo · sütun | Değer | Gerekçe |
   |---|---|---|
   | `employees.full_name` (`NOT NULL`) | `Deleted person` | `00003:41` |
   | `employees.first_name`, `last_name`, `employee_code`, `role`, `email` | `NULL` | nullable; CHECK'ler yalnız doluyken (`00037:63-79`) |
   | `employees.external_ref` | `location_id` doluysa `NULL`; değilse **`deleted-<employee id>`** | `employees_home_or_external_ref` (`:89-90`); biçim regex'ini geçer; UUID tenant içinde tekil (`:110-112`) |
   | `employee_passwords`, `employee_password_links` | satırlar silinir | |
   | `sessions` | iptal; `device_info` nullable ise `NULL` (kart ölçer) | |
   | `app_devices` | iptal; `device_label` **kalır** | sunucunun *"iPhone"/"Android"*'i (`appactivate.go:37`) |
   | `admin_users.full_name` | `Deleted person` | |
   | `admin_users.email` | `deleted-<admin_users id>@deleted.invalid` | tenant içinde tekil |
   | `admin_users.password_hash` | rastgele bcrypt özeti (`00102:565` deseni) | `00018`'in bcrypt CHECK'i |
   | `admin_users.status` | `disabled` | `00006:72-73` |
   | `admin_invites` (bekleyen) | `email` yer tutucu, `full_name` `Deleted person`, `cancelled_at` | `00077`'nin CHECK'leri |
   | `admin_identities` | `identity_erase_member` (madde 8) | `00069:168` |
   | `api_keys` | iptal | askı kesmez (§1.10) |
   | `oauth_grants`, `oauth_tokens` | iptal | askıda token'ın çalışıp çalışmadığı ölçülmedi — iptal güvenli taraf |
   | `webhook_endpoints` | kapatılır | askı gönderimi kesmez |
   | `punch_claims.note`, `punch_claim_decisions.note`, `transaction_reviews.note` | **kalır** | ekleme-yalnız (§1.11); hukukçu sorusuna eklenir |
   | `site_punches`, `punch_events` | **kalır** | K-D |
   | `transactions`, `audit_log`, `billing_periods`, `tags`, `tenants` satırı | kalır | kayıt ve fatura |
   | `admin_signup_claims` | **envanterde yok** | `tenant_id`'si yok ve kullanılmış satırlar süpürülür (`00078:123`; `signupconfirm.go:311`) |
   | yedekler | 30 günde düşer | K-D |

   **`mail.ValidRecipient` `.invalid` alan adını reddeder** (`internal/mail/compose.go:60`) — yer tutucular hiçbir zaman posta alamaz; test *[B3-N-L2]*.
10. **Sıra:** onay (operatör) → askı (`customer_request`) → silici (uygulama; isteyenin adresi **önce** okunur, bellekte) → `completed_at` → onay e-postası
    (`personNotices`; metin K-A + K-D).
11. **Metin** (hukukçu onaylı): 30 gün, silinen kişisel veri, kalanlar ve süresi (Q13), holding adları, yedekler, onay e-postası.

### 2.10 Demo işletme — K-C2

Kullanıcı, PR #49 ve Faz H birleştikten sonra **web `/signup`'ı** grubun başka bir şirketinin gerçek AB KDV numarasıyla doldurur, e-postadaki linkle onaylar;
işletme `/signup`'taki gibi doğar (VIES doğrularsa logo ve e-postalarda ad çizilir). **Operatör yolu, şema değişikliği yok.** İnceleyici *"Create a business
account"*'ta yine KDV adımında durur (bedel 11).

### 2.11 Sevk, migration sırası, taban, geri alma

- Kod kartları PR #49'dan sonra `main`'den açılan `m16-store` dalında; Faz H PR'ı o daldır.
- **Migration'lar yürütme sırasıyla, commit anında:** öneri **00118, 00119, 00120** (M16-51) → **00121** (M16-55: `business_deletion_requests`, `op_*`,
  tür CHECK'i, `resolve_pending_business_erasures`) → commit anındaki ilk serbest: M16-61 (`identity_erase_member`), M16-56 (koşullu). goose `-allow-missing`'siz.
- `Down`: 00121 açık ya da tamamlanmış talep varken reddeder; M16-61'inki fonksiyonu düşürür; M16-56'nınki dolu sütunda reddeder.
- **Taban ölçümü** ayrı bir kart (M16-62), Faz H birleştirmesinden **önce**; SigNoz eşiklerini o türetir.
- **Geri alma:** config PR → `git revert`; yasal yayım → önceki sürüm; `TAPPA_BUSINESS_DELETION` boşaltılır; anonimleştirilmiş veri geri gelmez.
- **3.2 yedeği:** Resolution Center cevabı + kapı.

## 3. Güvenlik değerlendirmesi

| Tehdit | Bu ADR'yle | Kalan |
|---|---|---|
| Parola doldurma | bütçeler, ret listesi, K iş, WorkGate, SigNoz | küresel tavan; plaketsiz `businessName` + `employeeName` |
| Adres değiştirerek ele geçirme | `set_email` / `link_email` bağı | — |
| Silme talebiyle yarışan parola kodu | talep bekleyen parola davetlerini iptal eder, `consume` `app_signin_revoked_at` doluyken reddeder | — |
| İşverenin eski davetiyle ikinci bağ | tüketimde kardeş iptali | — |
| Pencere dışına itme | en eski önce, süzgeçler `LIMIT`'ten önce | > 3 işletmeli kişi |
| Silme talebinin kötüye kullanımı | aktif owner, kanıt, ad yazdırma, e-postalar, operatör onayı ve iptali | tek owner'ın talebi işletmeyi siler |
| Operatörün kimlik verisine erişimi | **yok** — silici `tappa_app`, tek yeni `identity_*` yalnız `tappa_app` | — |
| BYPASSRLS rollerinin özet okuması | migration `DO` bloğu `REVOKE` eder, `scoped-access.sql` istisnası | **`ALTER DEFAULT PRIVILEGES` yeni tabloyu oluştuğu an açar** — `REVOKE` aynı migration'da olmazsa özet okunur; `admin_users.password_hash` bugün okunur (adlandırılmış bedel) |
| Yer tutucu adrese posta | `ValidRecipient` `.invalid`'i reddeder | — |
| Ingress log'unda token | erişim log'suz yol, token'sız 303, `no-referrer` | nginx hata log'u |

## 4. Bedeller

1. Yeni kimlik bilgisi türü. 2. Adres doğrulayıcıdır. 3. Holding çalışanı işletme başına parola. 4. Çıkış kutusu dayanıksız. 5. WorkGate takası genişler.
6. Mekân NAT'ı. 7. Bellekteki kovalar. 8. Uygulama modu sunum sözleşmesi. 9. Silme 30 gün. 10. Q55 (b)'nin istisnası. **11. İnceleyici *"Create a business
account"*'ta KDV adımında durur** (gerçek AB KDV numarası ister; K-C2 demo işletmeyi kullanıcı açar). 12. Davetli kişi 24 saatte tamamlamazsa yeni davet.
13. Owner-çalışan yönetici adresiyle parola kullanamaz. 14. Operatöre e-posta yok. 15. Parola davetinde Wi-Fi ipucu yok. 16. Çalışanın silme talebinden sonra
parolalı giriş yeni işveren daveti ister. 17. Holding kayıtlarında ad kalır (K-D). 18. `admin_users.password_hash` `tappa_readonly`'ce okunur (ADR 0048; değişmez).
19. Kimlik kapalı listesine bir fonksiyon (`identity_erase_member`).

## 5. Kırmızı çizgiler

§4.1, §4.2 değişmez. §4.3: ekleme-yalnız tabloların hiçbiri değişmez; anonimleştirme yalnız ekleme-yalnız olmayan kişi sütunlarında. §4.4: plaket bağı
`sun.Verify`. §4.5: yeni tablolar §6 beşlisiyle; operatör yalnız `op_*` ve **yalnız `business_deletion_requests`'te**; kimlik yüzeyine operatör dokunmaz.
§4.6: talep personeli durdurmaz. §4.7 + §7: parola, adres, link token'ı, kod, çerez hiçbir log / hata / `detail`'de yok; yeni sır yok.

## 6. Mağaza ve gizlilik

App Store *Contact Info → Email Address*; Play *Personal info → Email address*, silme sorusu *"Yes"*, tek URL `/account-deletion`; gizlilik bölüm 12: parola,
işletme kaydı, işletme silme (30 gün, envanter, K-D, yedekler, onay).

## 7. Karar verilmedi

Q55 … Q59; hukukçuya Q13 ve K-D sorusu. *Sonra:* tarayıcıda parolayla giriş, *Change password*, panelden *"Send app sign-in e-mail"*, iOS `webcredentials:`.

## 8. Uygulama

M16-50 (kabul; ADR 0049 / 0010 / 0021 (sır sütunları) / 0026 / 0031 (`identity_erase_member`) / 0039 / 0045 / 0022 / 0048 notları; ADR 0005 risk eki;
CLAUDE.md §5'in iki cümlesi) · M16-51 · M16-52 · M16-53 · M16-54 · M16-55 · M16-56 (koşullu) · M16-57 · M16-58 · M16-59 · **M16-60 düştü (K-C2)** · M16-61 ·
M16-62 (taban). ADR 0049 notu: *"YYYY-MM-DD: §2'nin link mekanizması ADR 0051'le yerini aldı; bütçeler, görünürlük ve iki kapı devredildi; 7 günlük bekleme
devralınmadı; metin silinmez"*.
