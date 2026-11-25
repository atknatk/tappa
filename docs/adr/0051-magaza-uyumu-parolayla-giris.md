# ADR 0051 — Mağaza uyumu: çalışanın e-posta + parolayla girişi, uygulamadan işletme kaydı, işletme hesabını silme

- **Durum:** **Kabul edildi (2026-11-25)** — kullanıcı, AskUserQuestion, M16-50 (*Kabul notu* aşağıda, §9). Yazımı: aynı gün sekiz denetim turunun bulgularıyla
  yeniden yazıldı (kimlikler köşeli parantezde: *[B…]* fizibilite/güvenlik, *[A…]* sıfır hafızalı yürüyüş). **ADR 0049 §2'nin link mekanizmasının yerini aldı**
  (ADR 0049'un durum satırı ve notu, 2026-11-25).
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
   **`tappa_app`'ın bugün çalıştırabildiği 29 `identity_*` fonksiyonu** *[B4-D2]* — GRANT'ları saymak 30 verir (`git grep "GRANT EXECUTE ON FUNCTION public.identity_" -- db/migrations | grep "TO tappa_app"`), ama `identity_create_for_signup`'ın yetkisi `00083:86`'da geri alındı; güncel sayı `grep -E '^function identity_' internal/db/privcatalog_test.go | grep -c 'execute=tappa_app'` → **29** (`privcatalog_test.go:486`):
   `accept_invite`, `accept_invite_new`, `change_email`, `claim_signup`, `confirm_window`, `consume_reset`, `consume_row_reset`, ~~`create_for_signup`~~ (geri alındı),
   `keep_all`, `link_by_email`, `link_legacy`, `memberships`, `open_reset`, `open_row_session`, `open_session`, `open_signup_claim`,
   `password_for_session`, `record_failed_login`, `relink_refused`, `request_email_change`, `request_verification`, `revert_email_change`,
   **`revoke_sessions`**, `set_password`, `sweep_events`, `sweep_signup_claims`, `switch_session`, `unlink_backfilled`, `verify_email`,
   `withdraw_reset`. **Bir kimliği bir işletmeden silen / anonimleştiren fonksiyon yoktur.** CLAUDE.md §4.5(3) sırsız kimlik yazarlarını **"adlandırılmış dört
   istisna"** diye sayar (`CLAUDE.md:240-252`), `cmd/tappa/claudemdidentity_test.go:47-51` dizgeleri pinler; `identity_revoke_sessions` canlı bir oturum özeti ister ve
   kişiyi bütün işletmelerden çıkarır (`00085:294-340`; `internal/adminauth/membership.go:66-67`). `admin_identities.email NOT NULL` (`00069:168`);
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
17. **Mağaza (okundu 2026-11-25)** — birebir alıntılar (`f9b972a5`'teki §1.18'den geri getirildi) *[A5-R2]*:
    - Apple *"Offering account deletion in your app"* (`https://developer.apple.com/support/offering-account-deletion-in-your-app/`): *"Offer to delete the entire account
      record, along with associated personal data … only offering to temporarily deactivate or disable an account is insufficient."* · *"Apps not operating in highly
      regulated industries should not require people to make a phone call, send an email, use a chat support flow, or go through other support flows."* · *"Inform the
      person how long it will take to delete the account and provide a confirmation when the deletion is complete."* · *"You may add steps to verify the identity of the
      person making the request, and to confirm that they want to delete the account"* · *"If you're legally required to retain some user information, let people know what
      information will and won't be retained."*
    - Google *"Understanding Google Play's app account deletion requirements"* (`support.google.com/googleplay/android-developer/answer/13327111`): *"provide a web link
      resource where users can request app account deletion and associated data deletion"*, *"The weblink must be functional"*, *"delete the user data associated with that
      app account"*, *"retain certain data for legitimate reasons such as security, fraud prevention or regulatory compliance"* + *"must clearly inform users about your data
      retention practices"*.
    - Google *"Provide information for Google Play's Data safety section"* (`…/answer/10787469`): *"Collect" means transmitting data from your app off a user's device*;
      formda *"Does your app provide a way for users to request deletion of their data?"*.
    - Ana oturumun okuması (M16-50 yeniden okur): 2.1(a), 3.1.1, 3.1.3(c), 3.2 (forum retleri, topluluk kaynağı), 4.8.

## 2. Karar

### 2.1 Kullanıcı kararları (2026-11-25) — sabit

1. İlk ekran *Sign in* + *Create a business account*. 2. Çalışan girişi e-posta + parola. 3. Bağ ilk plaket / QR dokunuşunda. 4. Uygulama içi kayıt
uygulama modunda biter. 5. İşletme silme owner'ın uygulama içi talebi. 6. Demo *"Taptime Demo"*; inceleyici demo çalışanın kimliğini yalnız konsollarda alır.
7. Önce Play iç testi; üretim PR #49'u, hukukçu onaylı metni, `TAPPA_APP_ANDROID_SIGNER_SHA256`'yı bekler.
8. **K-A** — silme 30 günde; operatör **onaylar ve askıya alır**, kişisel veriyi **uygulama** (`tappa_app`, tenant bağlamında) anonimleştirir; kayıtlar yasal saklamada;
   onay e-postası; hukukçu + Q13 kapı.
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
- **Gizli sütunlu tablolar BYPASSRLS rollerinden geri alınır** *[B3-N11, B4-D3]*: iki tabloyu yaratan migration, `CREATE TABLE`'lardan **sonra**, her rolü **ayrı ayrı**
  sınayan bir `DO` bloğuyla (`IF EXISTS (… rolname = 'tappa_readonly') THEN REVOKE ALL ON employee_passwords, employee_password_links FROM tappa_readonly; END IF;` ve
  aynısı `tappa_readwrite` için) yetkiyi geri alır (dev'de roller yok, blok bir şey yapmaz); **istisnanın kapsamı bütün yetkilerdir** (okuma ve yazma — `scoped-access.sql`
  `tappa_readwrite`'a `tappa_app`'ın yazmalarını da kopyalar); `scoped-access.sql`'e iki tablo için kalıcı istisna (yeniden koşu `GRANT SELECT ON ALL
  TABLES`'ı geri getirmesin diye) bir kapıdır. **`admin_users.password_hash`'in bugün `tappa_readonly`'ce okunur olması** ADR 0048'in kabul ettiği bir
  durumdur (BYPASSRLS okuyucu insan erişimi; bcrypt cost 12) — bu ADR onu değiştirmez, M16-50'nin notunda **adlandırılmış bedel** olarak yazılır.
- **ADR 0021 §1'in sır sütunları listesine** `employee_passwords.digest`, `employee_password_links.token_hash` eklenir; `privCatalogAllowed` (`privcatalog_test.go:283`)
  iki tablonun `tappa_app` ve `tappa_resolver` satırlarını alır *[A3-N25]*.
- Kod `internal/adminauth`'ta; ret listesi (lisansı uyumlu ≤ 10 000 giriş ya da ≤ 300 elle kalıp); telefonda parola yok.

### 2.3 İlk parola ve sıfırlama: tek akış

1. `POST /app/v1/password/request` gövde `{"email": string}` → her durumda `202`, boş gövde, `[250, 300] ms`, eşleşmeyene e-posta yok; kaynak kovası
   `429 {"error":"rate_limited"}` + `Retry-After` *[A3-N18]*.
2. **Kim:** (a) aktif ya da (b) işverenin canlı davetini taşıyan davetli (Q59); işletme env'de, owner ayarı açık, askıda değil; **kişinin
   `employees.app_signin_revoked_at`'i boş** (silme talebinden sonra yeniden giriş yeni bir **işveren** davetinin tüketimini ister — §2.9.1)
   *[B2-H1]*.
3. **Çözümleyici** `resolve_staff_password_targets(p_email citext, p_tenants uuid[], p_all boolean)` (`tappa_resolver`, `SECURITY DEFINER`,
   `SET search_path = pg_catalog, pg_temp`): **bütün süzgeçler `LIMIT`'ten önce — yönetici adresi dışlaması dahil** (`NOT EXISTS (SELECT 1 FROM
   admin_users a WHERE a.tenant_id = e.tenant_id AND a.email OPERATOR(public.=) e.email AND a.status = 'active')` — `tappa_resolver`'ın
   `admin_users (tenant_id, email, status)` yetkisi `00011:212-213`'ten beri var) *[A2-A10]*; sıra **`t.created_at, e.created_at, e.id`**
   (en eski işletme, en eski satır); `LIMIT 3`. Birinci tur önerisinin `(e.status <> 'active')` anahtarı **kalktı**: saldırganın kendi
   işletmelerindeki **aktif** satırlar davetli kurbanın önüne geçebiliyordu; yaş tek başına yeter *[B2-H2]*. Yeni yetkiler yalnız yeni sütunlar:
   `employees (id, tenant_id, email, status, created_at, app_signin_revoked_at)`, `tenants (suspended_at, staff_password_signin, created_at)`,
   Q59 (b) ise `employee_invites (origin)`.
4. **Link** `employee_password_links` (00118): 256 bit token, **`link_email citext NOT NULL`** (tüketimde `= employees.email` şartı — adres
   arada değiştiyse link ölür); HMAC anahtarı **yeni bir sır değil**: `SessionHMACKey`'den etiketli HMAC ile türetilir (`deriveInviteKey`'in
   deseni, etiket **`"tappa/staff-password-link/v1/key-derivation"`** — `admininvite.go:64`, `signupclaim.go:68`'in biçimi) *[B2-LOW, A3-low]*; **token
   CLAUDE.md §7'nin asla-loglanmaz listesindedir**; **yeni basım önce kişinin süresi dolmuş canlı linklerini kapatır** *[A3-N22]*; ömür 1 sa; kişi başına tek canlı link; çıkış kutusu; bütçeler
   `LockTenantForStaffPasswordLimits` altında.
5. **Sayfa** `GET/POST /staff/password`: GET token'ı kısa ömürlü bir çereze alır ve **token'sız** temiz adrese 303 verir; her cevap
   `Referrer-Policy: no-referrer` (`adminreset.go:909`, `accountemail.go:487` emsali) *[B2-O1]*; POST tek transaction'da: özet (`set_email` =
   güncel adres), link `used_at`, öteki linkler `revoked_at`, **kişinin bekleyen `origin='password'` davetleri** — yeni sorgu
   **`CancelPendingPasswordInvitesForEmployee`** (`WHERE origin = 'password'`) — ve kilit: **`LockTenantForInviteLimits`** (parolalı giriş basımıyla
   aynı kilit → eşzamanlı bir basım ya önce biter ve iptal edilir ya sonra gelir ve yeni özeti görür) *[A2-A18]*. Kişiye *"password set"*.
6. **Bildirimler** `personNotices` (kişiye; kuyruk 32, 2 işçi, `SendExempt`, kişi + tür başına günde 5). **Bütün yeni e-postalar `TenantVerified`
   kuralına uyar:** işletmenin adı yalnız KDV'si VIES doğrulanmışsa yazılır, aksi hâlde nötr metin (oltalama yolu) *[B2-O-d]*. 

### 2.4 Giriş: parola → aktivasyon daveti → ilk dokunuşta cihaz

1. Sıra: kaynak tavanı → `TryCharge` bütçeleri → `WorkGate` → aday çözümleyicisi → tam **K = 3** karşılaştırma → karar. **Tel sözleşmesi** *[A3-N18]*: istek
   `{"email": string, "password": string}`; `200 {"workplaces":[{"code": string (43 karakter base64url — `internal/invite/code.go:55-57`), "businessName": string}]}`
   (1–3 öğe) · `401 {"error":"signin_failed"}` · `429 {"error":"rate_limited"}` + `Retry-After` · `503 {"error":"busy"}` + `Retry-After: 60` · env boşken
   `404 {"error":"not_found"}` (`app.go:285`); `signin_failed` ve `busy` `appErrorStatus` haritasına girer.
2. **Aday çözümleyicisi** `resolve_staff_signin_candidates(p_email citext, p_tenants uuid[], p_all boolean)` — §2.3.3'ün süzgeçleri ve sırası **+
   `JOIN employee_passwords p ON … AND p.set_email OPERATOR(public.=) e.email`**, hepsi `LIMIT`'ten önce *[B2-H2]*.
3. **Doygunluk kehaneti kapalı** *[B2-H2]*: pencereye yalnız **parolası belirlenmiş** satırlar girer ve parola yalnız adresin kutusundan gelen
   linkle belirlenir; saldırganın kurban adresiyle açtığı satırlar parolasızdır ve pencereyi doldurmaz. Cevaptaki `workplaces` sayısı yalnız **doğru
   parolayı** bilene görünür ve yalnız o parolanın doğrulandığı işletmeleri sayar; bilinmeyen adres ile bilinen adres aynı 401'i alır. Kalan:
   aynı parolayı > 3 işletmede kullanan kişinin en yeni işletmesi listede yoktur (M16-50 sayar).
4. **Başarı bir oturum ya da cihaz değildir.** Doğrulanan her satır için o işletmenin `WithTenant`'ında `IssueAndDeliver` — **M16-52'nin işi
   olarak** `IssueParams`'a iki yeni alan: **`Origin`** (`"employer"` varsayılan / `"password"`) ve **`Gate func(ctx, q) error`** (basım
   transaction'ında, `LockTenantForInviteLimits`'ten sonra koşan kanca); `CreateInvite`'e (`invites.sql:66-71`) `origin` sütunu; `make gen`
   *[B2-O2, A2-S16]*. Değişmeyen tek şey **basımdaki iptal kuralıdır**: `CancelPendingInvitesForEmployee` iki kökeni de emekliye ayırır.
   Ömür **24 saat**, kod cevapta (`ManagerVisibleChannel` biçimi). M16-35'in *"`Gate` / `Origin` yalnız `*EmailChannel` ile"* kuralı ([G35-s])
   **uygulanmaz** — M16-35 yerini aldı.
5. **Tüketim kardeşleri iptal eder:** `consume`'da `ConsumeInviteAndActivate`'ten hemen sonra aynı transaction'da `CancelPendingInvitesForEmployee`
   (köken ayırmadan); bütün tamamlama yolları bu ortak adımdan geçer *[B-Y1]*. İşveren kökenli bir davet tüketilince kişinin
   `app_signin_revoked_at`'i temizlenir (aynı transaction); **`app_signin_revoked_at` doluyken `origin='password'` bir davet tüketilemez** (silme talebiyle
   yarışan bir koda karşı) *[B3-N-M2]*.
6. **Parola davetinin lookup'ı daraltılır** *[B2-O5]*: `origin = 'password'` davette `activate/lookup` yalnız `employeeName`, `businessName` ve markayı
   döndürür — **`locationName` ve `wifiSsid` dönmez** (plaketten önce mekânın ve ağının adı açılmaz; bedel: parolalı kurulumda Wi-Fi ipucu yok).
   `businessName` onay ekranında veri sorumlusu olarak gereklidir (GDPR Art. 13) ve kalır.
7. Görünürlük, owner'a saatlik özet, kişiye bildirim, onay listesi uyarısı, T59 (a) — M16-52. **Kurtarma yolu:** yeni parola → bekleyen parola
   davetleri ölür → giriş + dokunuş → Q40 (a) saldırganın cihazını iptal eder; ya da müdür *Sign out phones*.
8. Kilit yok; telafi bütçeler, ret listesi, uyarı, fiziksel bağ.

### 2.5 Bütçeler

Her bütçe karşılaştırmadan önce `TryCharge` ile ayrılır.

| Kova | Değer | Nerede |
|---|---|---|
| giriş — kaynak, başarısız | 10 / 10 dk | bellek |
| giriş — kaynak, karşılaştırmaya ulaşan | 30 / 10 dk (≈ 34 sn CPU — hesap) | bellek |
| giriş — hesap izi | 10 / 10 dk + tek `FirstOverLimit`, kapatmaz | bellek |
| parola daveti — kişi | 5 / sa (`origin='password'`) | DB, `LockTenantForInviteLimits` |
| link isteği — kaynak / süreç / işletme / kişi | 5 / 10 dk + 20 / gün · 100 / sa · 10 / sa + 30 / gün · 1 / sa + 3 / gün | bellek · bellek · DB · DB |
| parola sayfası — link başına geçersiz form | 10 / 10 dk | bellek |

`CountRecentInvites` `origin = 'employer'`'a daralır. Küresel tavan (hesap): ≈ 0,88 tahmin/sn, ≈ 76 000 / gün; o sırada panel girişleri 503.

### 2.6 Uygulama modu

- **Taşıma** *[A3-N13]*: `GET /signup?client=app` bayrağı sihirbazın **imzalı durum çerezine** (`signupstate.go`, yol `/signup`) yazılır; sonraki GET ve POST'lar onu
  oradan okur — form alanı ya da sorgu dizgesi taşımaz. **Bayrak taslağa yazılmaz** *[A4-D8]*: `admin_signup_claims_draft_closed` taslağın anahtarlarını kapatır
  (`00078:139-151`) ve bir migration gerekmez. **Onay linki e-postadan sistem tarayıcısında açılır** (uygulamanın dışında); `SignupDone` orada **bugünkü** sayfadır
  (*Sign in to your dashboard* düğmesiyle) — 3.1.1 uygulamanın içini sorar; uygulamanın içinden fiyata ya da panele bağlantı yoktur. Uygulama içindeki son ekran
  `/signup/sent`'in uygulama modu metnidir.
- **Kaybolan** (uygulama içi adımlarda): `publicchrome.templ:170`, `:180`, `:186`, `:197-205`, `:213`, `:281`; `signup.templ:290`, `:840`. (`:697` *Sign in to your
  dashboard* yalnız `SignupDone`'dadır, o sayfa sistem tarayıcısında açılır — uygulama modu oraya taşınmaz.)
  **Kalan:** `:277` (K-B), `:292` + yasal iç bağlantılar (`?client=app`), dış bağlantılar, `mailto:`, çapalar, **`signup.templ:765`** (onay linki — yalnız
  e-posta gönderimi kapalı sandbox/dev kurulumunda çizilir; sihirbazın kendi akışıdır). `/account-deletion?client=app` aynı testte.

### 2.7 İnceleyici bağı — Q55

Öneri (b): operatörün işaretlediği işletmede statik QR bağlar (bayrak **`tenants`'ta değil, kendi tenant kapsamlı tablosunda** — `store_review_flags` (`tenant_id`, `enabled_at`,
`disabled_at`, `set_by uuid` — operatörün `platform_admins.id`'si, `operator_audit_log`'un deseninde FK'siz; §6 beşlisi; `tappa_opdefiner`'a yalnız bu tabloda `SELECT, INSERT, UPDATE (disabled_at)`; `tappa_app`'a `SELECT`), yalnız `op_set_store_review` —
böylece ADR 0021'in `tenants` *"ASLA"* hücresi ve `opDefinerTenantsUpd63` premisi (`holdingops_test.go:743`, `operatorsuspend_test.go:92`) değişmez; silme talebinin deseni
*[A5-N12]*); (a) manager girişi;
(c) canlı QR; (d) reddedildi.

### 2.8 Açılış — Q56, ve birleştirmede ne canlıya çıkar

Öneri (b): env `TAPPA_STAFF_PASSWORD_TENANTS` + owner ayarı (eski kapalı, yeni açık; `signup.Provision` aynı transaction'da
`UPDATE` ile; `GRANT UPDATE (staff_password_signin) ON tenants TO tappa_app`). **Birleştirmede env'den bağımsız canlıya çıkan** *[B2-removal]*: (1) bütün migration'lar (yeni tablolar, sütunlar, çözümleyiciler,
~~`vat_number`'ın `NULL` olabilmesi~~ — K-C2'yle düştü); (2) Ingress'in yeni erişim log'suz yolları; (3) `CountRecentInvites`'in daralması ve tüketimdeki kardeş iptali
(**her işletmede** davranış: tüketim artık kalan davetleri iptal eder); (4) panelin owner ayarı; (5)
`/account-deletion`'ın çalışan bölümü. **Env'e bağlı:** `/app/v1/password/request`, `/app/v1/signin`, `/staff/password`. **Ayrı bayrağa bağlı:**
`/account-deletion`'ın owner formu (§2.9.3). KF/KM: 401 ve 404 *"Have an invitation code?"* metniyle; mağaza üretimi KF/KM owner kararını bekler.

### 2.9 İşletme hesabını silme — K-A, K-D; giriş yeri Q57

1. **Çalışanın kendi talebi — M16-52 yapar** *[B2-H1, B3-N-M2, A3-N12]*: `RequestDeletion` (`staffdeletion.go`) aynı transaction'da kişinin `employee_passwords`
   ve `employee_password_links` satırlarını siler, `app_signin_revoked_at`'i yazar ve **`CancelPendingPasswordInvitesForEmployee`**'yi çağırır.
2. **Sayfa** `/account-deletion` — iki bölüm (çalışan yolu + owner formu); owner formu env **`TAPPA_BUSINESS_DELETION`** arkasında: **değer sözdizimi** —
   boş ya da tanımsız = kapalı; `on` = açık; başka her değer açılışı reddeder (`TAPPA_INVITE_DELIVERY`'nin kapalı küme kuralı) *[A3-N19]*. Bayrak, M16-61
   üretimde çalışana dek açılmaz; mağaza üretim gönderimi bayrağı bekler; Faz H PR'ı Q13 / hukukçu beklerken birleşebilir. **Bayrağı boşaltmak onaylanmış
   talepleri durdurmaz** — yalnız yeni talebi kapatır *[B4-rollback]*.
3. **Kanıt** `adminauth.Manager.ProveOwnership` (yeni; oturumsuz). **Owner formunun adımları:** adım 1 e-posta + parola (POST) → adım 2 işletme seçimi + adını
   yazma (POST) → bitti; adımlar arası HMAC'li 10 dakikalık çerez (kanıtın kimliği + izinli tenant id'leri), senkronizasyon token'ı (çerez + alan), Origin;
   `no-referrer`. **Asla loglanmaz:** e-posta, parola, çerez değeri, token.
4. **Talep yeni, tenant kapsamlı bir tabloda** *[B3, A3-N7, B4-O3]*: **`business_deletion_requests`** — `id`, `tenant_id`, `requested_by` (owner'ın
   `admin_users.id`'si), `requested_at`, `approved_at`, `completed_at`, `cancelled_at`, **`confirm_to citext`** (onay e-postasının alıcısı — madde 10), **`confirm_attempts
   smallint`** (≤ 5); §6 beşlisi; işletme başına tek açık talep (kısmi `UNIQUE (tenant_id) WHERE completed_at IS NULL AND cancelled_at IS NULL`). Önceki turun
   `prior_suspended_at` / `prior_suspension_reason` sütunları **kalktı**: onay askıyı **yalnız `suspended_at IS NULL` iken** koyar ve iptal askıyı **yalnız güncel neden
   `customer_request` ise** kaldırır (madde 6), bu yüzden önceki durum saklanmaz *[B7-M2]*.
   **Durum makinesi:** `tappa_app`'a **sütun düzeyi** `INSERT (id, tenant_id, requested_by, requested_at, confirm_to)`; `BEFORE INSERT` tetikleyicisi üç zaman damgasının
   `NULL` olmasını ister; `BEFORE UPDATE` tetikleyicisi: her zaman damgası yalnız `NULL` → değer; `approved_at` yalnız `cancelled_at IS NULL`; `completed_at` yalnız
   `approved_at IS NOT NULL AND cancelled_at IS NULL`; `cancelled_at` yalnız `completed_at IS NULL`; **`confirm_to` yalnız değer → `NULL`** (hiçbir zaman yeniden yazılmaz)
   *[B6-low4]*; iptal `confirm_to`'yu `NULL`'a çeker. **Yetkiler — tam liste** *[A6-D5]*: `tappa_app`'a `SELECT`, `INSERT (id, tenant_id, requested_by, requested_at,
   confirm_to)`, `UPDATE (completed_at, confirm_to, confirm_attempts)`; **`tappa_opdefiner`'a yalnız bu tabloda `SELECT` ve `UPDATE (approved_at, cancelled_at)`**;
   `tappa_identitydefiner`'a `SELECT (id, tenant_id, approved_at, completed_at, cancelled_at)` (madde 8). `tenants`'ta yeni UPDATE sütunu **açılmaz**, ADR 0021'in
   *"ASLA"* hücresi değişmez; ADR 0021 §1'in `tappa_opdefiner` *"Tutar"* hücresine `business_deletion_requests (SELECT; UPDATE (approved_at, cancelled_at))` eklenir.
   `tenants`'a sütun eklenmez.
5. **Talep transaction'ı (tappa_app):** satır + `tenant.deletion_requested` audit + **isteyen owner'ın bu işletmedeki üyeliği için `RevokeAdminSessionsForAdmin`**
   (`admins.sql:203` — var olan sorgu; yeni bir `RevokeMembershipSessions` **yok**) *[A3-N20]*. **`identity_revoke_sessions` kullanılmaz** *[B4-O2]*: canlı bir oturum
   özeti ister ve kişiyi **bütün** işletmelerinden çıkarır (`00085:294-340`; `internal/adminauth/membership.go:66-67`). Kapanan oturumlar: **isteyen owner'ın bu
   işletmedeki** panel oturumları — öteki owner'ların, personelin ve kişinin öteki işletmelerdeki oturumları kapanmaz. Sonra isteyene ve öteki owner'lara e-posta.
6. **Operatör** *[B4-D6, A4-D12, B6-M1, B6-M3, B7-M1, B7-M2, B7-M3b]*. **Bütün yazma `op_*`'ları ADR 0021'in kuralına uyar** (`docs/adr/0021-*.md:558-565`): **yalnız
   `void`** döndürür; önceden var olan duruma bağlı hiçbir dönüş ya da hata yoktur (pin: `internal/db/operatorschema_test.go:383-386`, `operatorsuspendcalls_test.go:242-248`).
   **İmza** `(p_session, p_tenant_id, p_request_id)`; satır `id = p_request_id AND tenant_id = p_tenant_id` ile eşlenir. **`FOUND` deseni:** **her** kabul edilen çağrıda,
   `p_tenant_id`'yi adlandıran bir operatör satırı yazılır (`operatorsuspendcalls_test.go:245-246`'nın emsali — bilinmeyen id'de de); yalnız **etki** duruma bağlıdır.
   Durum filtreleri `op_*`'ın `WHERE`'indedir, bu yüzden tek yönlü tetikleyici bu çağrılarda **hiçbir zaman** hata vermez. Handler çifti biletli okumadan alır. **Kilit sırası:**
   talep satırı → `tenants`.
   - `op_read_deletion_requests(p_session, p_ticket)` (yeni) döndürür: `id`, `tenant_id`, `tenant_name`, `requested_at`, `approved_at`, `completed_at`, `cancelled_at`,
     `confirm_pending boolean` (`confirm_to IS NOT NULL` — adres asla), `confirm_attempts`, `days_left`; en eski önce. Okuma bileti türü **`deletion_requests`**
     (`operator_read_tickets_kind_check`); `op_begin_read` ve `op_read_audit` yeni türleri tanıyacak biçimde yeniden tanımlanır — `00107`'nin koşullu `ADD` + ön koşul deseni,
     `00107:144-151`; `00029:137-139` *[B6-low1]*.
   - `op_approve_business_deletion(p_session, p_tenant_id, p_request_id)` → `void`: açık talepte `approved_at`; **tenant askıda değilse** (`suspended_at IS NULL`) aynı
     transaction'da `op_suspend_tenant`'ın gövdesindeki askı (`customer_request`, `00063:354`); **askıdaysa iki sütuna da dokunmaz** *[B7-M2]*. Zaten onaylanmış, iptal
     edilmiş ya da tamamlanmış talepte etki yok, yalnız operatör satırı.
   - `op_cancel_business_deletion(p_session, p_tenant_id, p_request_id)` → `void`: tamamlanmamış talepte `cancelled_at`; talep onaylanmışsa ve **güncel neden hâlâ
     `customer_request` ise** askı kalkar (`op_reinstate_tenant`'ın gövdesi); neden başkaysa (arada başka bir askı — `op_suspend_tenant` nedeni ezer, `00063:975-978`) durum
     **olduğu gibi kalır** ve operatör ekranı söyler *[B6-M3]*. **Kalan iki bedel** *[B7-M2]*: (i) onaydan önce **zaten `customer_request` ile** askıda olan bir işletme iptalde
     açılır; (ii) onaydan sonra operatörün **ayrıca `customer_request` ile** koyduğu bir askı iptalde kalkar.
   - `op_renotify_business_deletion(p_session, p_tenant_id, p_request_id)` → `void` *[A5-N11, B7-M1]*: **her** çağrıda operatör satırı `business_deletion_renotified`; handler
     **etkiyi** biletli okumadaki duruma göre seçer — onaylı, iptal edilmemiş ve tamamlanmamış → silme bildirimi; tamamlanmış ve `confirm_pending` → yalnız-gönderim bildirimi;
     başka her durum → bildirim yok.
   - `op_reinstate_tenant` (`00063:1015`) yeniden tanımlanır: `tenants` kilidinden **sonra** silme talebini **düz bir okumayla** sınar; `approved_at IS NOT NULL AND cancelled_at IS
     NULL` bir talep varken **sessiz no-op** — aynı `void`, tenant'ta değişiklik yok, yalnız operatör satırı *[B6-M1, B7-M3b]*. Bu, K15-11'in *"satır uygulandığını söyler, gövde
     önceki durumu okumaz"* kuralından (`00063:1011-1013`) **adlandırılmış bir sapmadır**; bugünkü pinli testler (`operatorsuspendcalls_test.go:227-240`) silme satırı olmayan
     tenant'larla koştuğu için **yeşil kalır**.
   - Yeni `operator_audit_log` türleri: `business_deletion_read`, `business_deletion_approved`, `business_deletion_cancelled`, `business_deletion_renotified` —
     `operator_audit_log_kind_check` `00107`'nin deseniyle genişler; tenant'a yazan üçü `operator_audit_log_tenant_write_shape`'e (`00063:423-428`) girer *[B6-low1]*.
7. **Anonimleştirme uygulama tarafında, anahtarlı tetikle** *[B3, B4-O5, B4-D8]*: **süpürücü YOK** — `resolve_pending_business_erasures()` önerisi **geri çekildi**
   (anahtarsız, tenant'lar arası bir tarama K-19'a — `internal/webhook/worker.go:7-14` — ve ADR 0002 md. 7'nin anahtarlı çözümleyici desenine ters düşerdi). Bunun
   yerine: `op_approve_business_deletion` commit edince operatör handler'ı tüketici tarafı bir **bildirim** arayüzüne (`ErasureNotice`, `internal/handler/operator`'da;
   uygulaması uygulama tarafında bir süreç içi kuyruk — `cmd/tappa` bağlar) `(tenant_id, request_id)` verir; **operatör yüzeyine havuz geçmez** — yalnız iki kimlik
   (ADR 0021 §3.6'nın *"operatörün havuzu yalnız operatör tarafına"* kuralı ve `TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator` (`cmd/tappa/operator_test.go:636`;
   `cmd/tappa/operator.go:33-42`) değişmez; ADR 0021'e not: *"operatör yüzeyi uygulama tarafına bir kimlik çifti bildirir, yazma yetkisi almaz"*). Süreç ölürse kuyruk
   boşalır; operatör listesi *"approved, not completed"* satırını gösterir ve **"Run the erasure"** düğmesi aynı bildirimi yeniden verir — K-19'un
   *"bekleyen iş bir sonraki olaya kadar uyur"* bedeli. **Silici transaction'ı** (`tappa_app`, `SET LOCAL app.tenant_id = <bildirimin tenant'ı>`): ilk ifade
   `SELECT … FROM business_deletion_requests WHERE id = $request AND tenant_id = $tenant AND approved_at IS NOT NULL AND completed_at IS NULL AND cancelled_at IS NULL
   FOR UPDATE` — satır yoksa geri alır; satır kilidi aynı bildirimi iki pod'da (rolling update — `replicas: 1`, `maxSurge: 1`) seri yapar, ikincisi tamamlanmış satırı
   bulur ve hiçbir şey yazmaz. Bitince tenant'ın `audit_log`'una `tenant.deletion_completed` satırı. **Hiçbir operatör rolü kimlik fonksiyonu ya da tablosuna dokunmaz;**
   `identitycatalog_test.go:408-415`, `adminidentity_test.go:179`, `:283` değişmez.
8. **Kimlik tarafı — `identity_erase_member` ve `identity_erase_tenant_invites` = BEŞİNCİ adlandırılmış istisna** *[B4-Y1, B4-O1]*. Sırsız bir kimlik yazarıdır; CLAUDE.md §4.5(3) bugün
   *"adlandırılmış dört istisna"* der (`CLAUDE.md:240-252`) ve `cmd/tappa/claudemdidentity_test.go:40-48` iki belgeyi karşılaştırıp dizgeleri arar. **M16-50'nin kullanıcı
   kapısı:** beşinci istisna + CLAUDE.md §4.5(3)'ün metin farkı (*"dört"* → *"beş"*, listeye **şu birebir metin** *[B5-low5]*: *"`identity_erase_member` ve `identity_erase_tenant_invites` (yalnız işletmenin kendi bağlamında, o işletmede onaylanmış ve açık bir silme talebi varken ve yalnız o işletmenin satırları için; Go tipiyle bağlı silici çağırır)"*) + ADR 0031 §6.1'in aynı metni +
   `claudemdidentity_test.go`'nun dizgeleri + tanımcının yeni yetkileri.
   **Yapısal yüklemler** (`identity_create_for_signup`'ın emsali, ADR 0031 `:309`): (1) `p_tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid`;
   (2) `business_deletion_requests`'te o tenant'ın `approved_at IS NOT NULL AND cancelled_at IS NULL AND completed_at IS NULL` bir satırı **kilitsiz, düz bir `SELECT`**
   ile yeniden sınanır — `FOR UPDATE` / `FOR SHARE` `UPDATE` yetkisi ister, sütun `SELECT`'i yetmez (denetimin PG17 ölçümü) *[B5-O1]*; **kilit Go silicisinin aynı
   transaction'daki `FOR UPDATE`'idir**, son bekçi `completed_at` tetikleyicisidir; (3) `p_admin_user_id`'nin satırı `p_tenant_id`'ye aittir.
   **Kilit sırası** *[B5-O3, B7-M3a]*: üye başına kimlik `FOR UPDATE` → kimliğin **bütün** üyelik satırları `FOR UPDATE` (`00072:985-990`) → karar; işletme düzeyinde **bütün
   üyelik satırları → davetler** (davetler ancak bütün üyeler işlendikten sonra).
   **Sözleşme** (`tappa_app` `admin_users.email`'i yazamaz — `00069:251` — ve bağlı satırların e-posta / özetini yalnız `tappa_identitydefiner` yazar — `00069:264-301`):
   satırın `email` → `deleted-<admin_users id>@deleted.invalid`, `password_hash` → `00102:565`'in rastgele bcrypt özeti, `status` → `disabled`, **`unlinked_at` → `now()`**
   (üyeliği kapatmak = bağı koparmak; `identity_id` tarih için kalır; bağ koptuğu için K-14 aynası artık bu satıra yazmaz — `00102:405-412`, `:575`). **"Başka bağlı
   üyelik"** = aynı `identity_id`'li, `unlinked_at IS NULL` herhangi bir satır, durumu ne olursa olsun *[A4-D9]*; yoksa kimlik anonimleşir: `admin_identities.email` →
   `deleted-<identity id>@deleted.invalid`, `password_hash` → rastgele özet, canlı `admin_identity_resets` ve `admin_identity_verifications` → **`cancelled_at`** (tanımcının var
   olan `UPDATE (used_at, cancelled_at)`'ları — `privcatalog_test.go:295`, `:299`; **DELETE yok**, ADR 0031 §5.1'e istisna ve `identityPersonTableDeleters` değişikliği
   gerekmez) *[A5-N2]*. **Yönetici davetleri ayrı bir adımdır** *[B7-M3a]*: üye başına fonksiyon davetlere **dokunmaz**; bütün `admin_users` satırları işlendikten sonra
   **ikinci tanımcı fonksiyon `identity_erase_tenant_invites(p_tenant_id uuid) RETURNS void`** (aynı beşinci istisnanın sözleşmesi, aynı yüklemler 1–2) tenant'ın **her**
   `admin_invites` satırını (kullanılmış, iptal edilmiş, bekleyen) `email` → `deleted-<invite id>@deleted.invalid`, `full_name` → `Deleted person` yapar (`email_ascii`,
   `filled`, `≤ 120` — `00077:154-163`), bekleyenleri `cancelled_at` ile kapatır *[B5-Y1]*; tanımcıya **`UPDATE (email, full_name, cancelled_at) ON admin_invites`** (bugün yalnız
   `used_at` — `privcatalog_test.go:305`); **`tappa_app`'a verilmez** (`identity_accept_invite` davet adresini kimlik adresiyle eşler — `00085:676`, `:712`).
   `identity_erase_member(p_tenant_id uuid, p_admin_user_id uuid) RETURNS void`. Fonksiyon tenant'ın `audit_log`'una `admin.erased` yazar; `detail` kimliğin anonimleşip anonimleşmediğini söylemez *[B5-low3]*.
   **Fonksiyondan SONRA, aynı transaction'da, `tappa_app`** *[B6-M2]*: satırın `full_name` → `Deleted person` (tanımcının `full_name` yetkisi yok — `privcatalog_test.go:321`;
   `tappa_app`'ınki var — `:318`) ve `RevokeAdminSessionsForAdmin`. Önce fonksiyon, sonra yazmalar: ters sıra (önce `tappa_app` yazması) kimlik → üyelik kilit sırasıyla
   çakışır ve PG17'de **kilitlenme** ölçüldü. `admin_identity_events` ekleme-yalnız değildir — tanımcı 90 günde süpürür, satırlar `detail`'de tenant'ı 90 gün taşır; silme
   metni söyler (*"sign-in security events are kept for 90 days"*) *[B5-low9]*.
9. **Envanter — sütun başına değer** *[B3, A3-N8–N10, B3-N-L1, B4-O1, B4-O4, B4-D10, B4-D11]*:

   | Tablo · sütun | Değer | Gerekçe |
   |---|---|---|
   | `employees.full_name` (`NOT NULL`) | `Deleted person` | `00003:41` |
   | `employees.first_name`, `last_name`, `employee_code`, `role`, `email` | `NULL` | nullable; CHECK'ler yalnız doluyken (`00037:63-79`) |
   | `employees.external_ref` | `location_id` doluysa `NULL`; değilse **`deleted-<employee id>`** | `employees_home_or_external_ref` (`:89-90`); regex; tekil (`:110-112`) |
   | `employees.status` | **`deactivated`** (var olan `DeactivateEmployee` yolu) | faturalanabilirlik `tappa_employee_is_billable(status, …)`'a bağlı (`00016:219`); askıdaki işletmenin faturası ayrıca M16-61'de ölçülür — fark varsa kart durur. **ADR 0010'a adlandırılmış istisna** *[B5-O2]*: deaktivasyon bugün hiçbir şeyi iptal etmez (`db/queries/employees.sql:735-739`, `internal/domain/tenant/staff.go:809-813`); silinen işletmenin personelinin oturumları ve cihazları ise iptal edilir — sonraki dokunuş iptal edilmiş oturum dalına düşer, kayıt ve uyarı yazmaz; M16-50 AskUserQuestion ile sorar (*"an erased business's staff are signed out"*) |
   | `employee_passwords`, `employee_password_links` | satırlar silinir | |
   | bekleyen `employee_invites` (iki köken), `invite_requests` (`00037:183`), `activation_requests` (`00115:65`), `password_resets` (`00006:173`) | `employee_invites`, `activation_requests`, `password_resets` iptal işaretiyle (yetkileri M16-61 ölçer); **`invite_requests` `DELETE`** — iptal sütunu yok, `tappa_app`'ın `DELETE`'i var (`privcatalog_test.go:362-363`, `:660`) | askı aktivasyonu ve tap'i durdurmaz |
   | `sessions` | iptal (`revoked_at`) | `device_info` **envanterden çıktı**: `tappa_app` yalnız `last_used_at, revoked_at` yazabilir (`00099:90`) ve sütun sunucunun kaba etiketidir |
   | `app_devices` | iptal; `device_label` kalır | `appactivate.go:37` |
   | `admin_users` (her satır) | önce `identity_erase_member` (`email`, `password_hash`, `status`, `unlinked_at`), **sonra** `tappa_app` `full_name` `Deleted person` (madde 8, B6-M2) | `00069:251`, `:264-301`; `privcatalog_test.go:318`, `:321` |
   | `admin_users` oturumları | her satır için, fonksiyondan **sonra** `RevokeAdminSessionsForAdmin` | `admins.sql:203` |
   | `admin_invites` (**her satır**) | `email` `deleted-<invite id>@deleted.invalid`, `full_name` `Deleted person`; bekleyenler `cancelled_at` — **`identity_erase_tenant_invites`**, bütün üyelerden sonra (madde 8) | DELETE yalnız `tappa_owner`'da; `00077:154-163` |
   | `admin_identities`, `admin_identity_resets`, `admin_identity_verifications` | madde 8 — kimlik yalnız başka bağlı üyelik yoksa; resets ve verifications `cancelled_at` (DELETE yok) | `privcatalog_test.go:295`, `:299` |
   | `api_keys` | iptal | askı kesmez |
   | `oauth_grants`, `oauth_tokens` | her grant için `DisconnectOAuthGrant`, aktör `requested_by` (`revoked_by` FK'si — `00104:209`) | |
   | `webhook_endpoints` | kapatılır | askı gönderimi kesmez |
   | `tenants.staff_password_signin` | `false` | silinmiş işletmede parolalı giriş ve T59 (a)'nın `tap.signed_out_phone` satırı kapanır |
   | `punch_claims.note`, `punch_claim_decisions.note`, `transaction_reviews.note` | kalır | ekleme-yalnız; hukukçu sorusu |
   | `site_punches`, `punch_events` | kalır | K-D |
   | `transactions`, `audit_log`, `billing_periods`, `tags`, `tenants` satırı | kalır | |
   | `admin_signup_claims` | envanterde yok | `tenant_id`'si yok, kullanılmış satırlar süpürülür (`00078:123`; `signupconfirm.go:311`) |
   | yedekler | 30 günde düşer | K-D |

   **`.invalid` kuralı alıcı yolundadır** *[B4-D1, B5-low8, A6-D4]*: **alıcı yolu (dışa açık `ValidRecipient` — `internal/mail/compose.go:60` — ve `Send`'in `To`'su) `.invalid`'i
   reddeder; iç `validRecipient` (`compose.go:39`; `From` / `Reply-To` için de kullanılır) değişmez.** Yeni yardımcı `refusesReservedDomain(addr)`; `ValidRecipient(to) =
   validRecipient(to) && !refusesReservedDomain(to)` (sözleşmesi `Send`'le aynı cevap — `internal/mail/doc.go:90-94`; `TestValidRecipient_AgreesWithSend`'e `.invalid` vakası).
   `config.mailProbe`'un `probe@relay.invalid` göndereni (`internal/config/config.go:1797-1803`; `mailRule` `:1810`) iç yoldan geçer, açılış kırılmaz. Depoda **alıcı** olarak
   `.invalid` adresi yoktur (ölçüldü: dokuz eşleşme DSN, Tailscale ya da gönderen fikstürü); `internal/config/loaderrors_test.go:90`'ın `canary@mailbox.invalid`'i bütün
   `TAPPA_*` değişkenlerine verilen bir **hata metni sızıntısı** fikstürüdür — `config` dışa açık `ValidRecipient`'ı çağırmaz (ölçüldü: `grep`), posta değişkenleri iç yoldan
   geçer, sonucu değişmez *[B6-low8]*. Testler [H61-g] (`To` `x@deleted.invalid` → ret) ve [H61-j] (yapılandırma testleri yeşil).
10. **Sıra ve onay e-postası** *[B4-D6, B4-D7]*: onay + askı (operatör, tek transaction) → bildirim → silici (uygulama) → `completed_at` → onay e-postası
    (`personNotices`; metin K-A + K-D). **Adres düz metindir** (bu kod tabanında *"mühürlü"* KEK'le şifreli demektir; burada değil) *[B5-low1]*: `confirm_to` talep anında yazılır ve **gönderim başarıyla bitince**
    `NULL`'a çekilir; **iptalde tetikleyici `NULL`'a çeker**; gönderim en çok **5** kez denenir (`confirm_attempts`); 5. başarısız denemeden sonra `confirm_to` → `NULL` ve *"confirmation failed"* = `confirm_attempts = 5 AND confirm_to IS NULL`
    *[B6-low3]*; **gönderim talep satırının kilidi altında yapılır** (`FOR UPDATE` → gönder → `confirm_to = NULL` → commit), bu yüzden "Run the erasure" ile yalnız-gönderim yolunun yarışı
    çift e-posta doğurmaz; bedeli kilidin gönderim süresince (en çok `invite.EmailSendGrace`, 10 sn) tutulmasıdır *[B6-low6]*;
    `op_read_deletion_requests` adresi **asla** döndürmez, yalnız `confirm_to IS NOT NULL`'ı; `tappa_readonly`'nin bu sütunu okuyabilmesi `employees.email`'le aynı sınıftır
    (ADR 0048); **yalnız-gönderim yolu:** tamamlanmış (`completed_at IS NOT NULL`) ve `confirm_to IS NOT NULL` satırda silici yazmaz, yalnız `ResendDeletionConfirmation`'ı koşar *[A5-N11]*; süreç gönderimden önce
    ölürse operatör listesi *"completed, confirmation not sent"* gösterir ve *"Run the erasure"* yalnız gönderimi yeniden dener. Bedel: adres en çok 30 gün bu tabloda
    kalır (silinecek kişinin kendi adresi).
11. **Metin** (hukukçu onaylı): 30 gün, silinen kişisel veri, kalanlar ve süresi (Q13), holding adları, yedekler, onay e-postası.

### 2.10 Demo işletme — K-C2

Kullanıcı, **PR #49 birleştikten sonra ve Faz H'den önce** (M16-40'ın üretim turu demo işletmeyi ister — Faz H kapanış sırası adım 4) **web `/signup`'ı** grubun başka
bir şirketinin gerçek AB KDV numarasıyla doldurur, e-postadaki linkle onaylar; işletme `/signup`'taki gibi doğar (VIES doğrularsa logo ve e-postalarda ad çizilir).
**Operatör yolu, şema değişikliği yok.** Q56 (b)'de demo işletme Faz H'den **önce** kaydolduğu için **"eski" bir işletmedir**: `staff_password_signin = false` doğar
ve **owner panelden açar** (M16-57 adım 7). İnceleyici *"Create a business account"*'ta yine KDV adımında durur (bedel 11).

### 2.11 Sevk, migration sırası, taban, geri alma

- Kod kartları PR #49'dan sonra `main`'den açılan `m16-store` dalında; Faz H PR'ı o daldır. **Faz H PR'ı Q13 / hukukçu beklerken birleşirse M16-61 sonraki bir
  dalda ve PR'da gelir** (öneri dal `m16-store-erasure`, `main`'den; numarası commit anında) *[A4-D18]*.
- **Migration'lar yürütme sırasıyla, commit anında:** öneri **00118, 00119, 00120** (M16-51) → **00121** (M16-55: `business_deletion_requests`, `op_*`, iki tür
  CHECK'i, `op_reinstate_tenant`'ın yeni tanımı) → commit anındaki ilk serbest: M16-61 (`identity_erase_member` ve `identity_erase_tenant_invites`), M16-56 (koşullu). goose `-allow-missing`'siz.
- `Down`: 00121 açık ya da tamamlanmış talep varken reddeder ve `op_*`'ların önceki tanımlarını birebir geri koyar; **M16-61'inki onaylanmış ve tamamlanmamış bir talep varken
  reddeder**, yoksa iki fonksiyonu ve yetkileri düşürür *[B4-rollback]*; M16-56'nınki **açık bir bayrak** (`store_review_flags`, `disabled_at IS NULL`) varken reddeder ve
  `op_read_tenant_detail`'in önceki tanımını geri koyar.
- **Taban ölçümü** ayrı bir kart (M16-62), Faz H birleştirmesinden **önce**; SigNoz kurallarının sahibi de M16-62'dir.
- **Geri alma:** config PR → `git revert`; yasal yayım → önceki sürüm; `TAPPA_BUSINESS_DELETION` boşaltılır (onaylanmış talepleri durdurmaz); anonimleştirilmiş veri geri
  gelmez; M16-61 `Down`'ı yukarıdaki kuralla.
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
| Operatörün kimlik verisine erişimi | **yok** — silici `tappa_app`, iki yeni `identity_*` fonksiyonu (`identity_erase_member` ve `identity_erase_tenant_invites`) yalnız `tappa_app`'ça çalıştırılır; operatör yalnız bir kimlik çifti bildirir | **beşinci adlandırılmış istisna** (`identity_erase_member` ve `identity_erase_tenant_invites`, yapısal yüklemlerle) — CLAUDE.md §4.5(3) değişir |
| BYPASSRLS rollerinin özet okuması | migration `DO` bloğu `REVOKE` eder, `scoped-access.sql` istisnası | **`ALTER DEFAULT PRIVILEGES` yeni tabloyu oluştuğu an açar** — `REVOKE` aynı migration'da olmazsa özet okunur; `admin_users.password_hash` bugün okunur (adlandırılmış bedel) |
| Yer tutucu adrese posta | dışa açık `ValidRecipient` ve `Send`'in `To` yolu `.invalid`'i reddeder; iç `validRecipient` (gönderen) değişmez | — |
| Ingress log'unda token | erişim log'suz yol, token'sız 303, `no-referrer` | nginx hata log'u |

## 4. Bedeller

1. Yeni kimlik bilgisi türü. 2. Adres doğrulayıcıdır. 3. Holding çalışanı işletme başına parola. 4. Çıkış kutusu dayanıksız. 5. WorkGate takası genişler.
6. Mekân NAT'ı. 7. Bellekteki kovalar. 8. Uygulama modu sunum sözleşmesi. 9. Silme 30 gün. 10. Q55 (b)'nin istisnası. **11. İnceleyici *"Create a business
account"*'ta KDV adımında durur** (gerçek AB KDV numarası ister; K-C2 demo işletmeyi kullanıcı açar). 12. Davetli kişi 24 saatte tamamlamazsa yeni davet.
13. Owner-çalışan yönetici adresiyle parola kullanamaz. 14. Operatöre e-posta yok. 15. Parola davetinde Wi-Fi ipucu yok. 16. Çalışanın silme talebinden sonra
parolalı giriş yeni işveren daveti ister. 17. Holding kayıtlarında ad kalır (K-D). 18. `admin_users.password_hash` `tappa_readonly`'ce okunur (ADR 0048; değişmez).
19. Kimlik kapalı listesine **iki fonksiyon** (`identity_erase_member` ve `identity_erase_tenant_invites`) ve **CLAUDE.md §4.5(3)'e beşinci istisna**. 20. Süreç onay ile silme arasında ölürse silme
    operatörün *"Run the erasure"* düğmesine dek bekler (K-19'un bedeli). 21. Onay e-postasının adresi gönderilene dek tabloda **düz metin** kalır (en çok 5 deneme). 22. Tenant'ın **her** yönetici daveti satırının adresi ve adı yer tutucuya çevrilir (`identity_erase_tenant_invites` — B5-Y1, B7-M3a, [H61-p]); satırlar DELETE edilmez (yalnız `tappa_owner`).

## 5. Kırmızı çizgiler

§4.1, §4.2 değişmez. §4.3: ekleme-yalnız tabloların hiçbiri değişmez; anonimleştirme yalnız ekleme-yalnız olmayan kişi sütunlarında. §4.4: plaket bağı
`sun.Verify`. §4.5: yeni tablolar §6 beşlisiyle; operatör yalnız `op_*` ve yeni yetkiyi **yalnız `business_deletion_requests`'te** (Q55 (b) ise ayrıca `store_review_flags`'te) alır; kimlik yüzeyine operatör dokunmaz.
§4.6: talep personeli durdurmaz. §4.7 + §7: parola, adres, link token'ı, kod, çerez hiçbir log / hata / `detail`'de yok; yeni sır yok.

## 6. Mağaza ve gizlilik

App Store *Contact Info → Email Address*; Play *Personal info → Email address*, silme sorusu *"Yes"*, tek URL `/account-deletion`; gizlilik bölüm 12: parola,
işletme kaydı, işletme silme (30 gün, envanter, K-D, yedekler, onay).

## 7. Karar verilmedi

Q55 … Q59; hukukçuya Q13 ve K-D sorusu. *Sonra:* tarayıcıda parolayla giriş, *Change password*, panelden *"Send app sign-in e-mail"*, iOS `webcredentials:`.

## 8. Uygulama

M16-50 (kabul; ADR 0049 / 0010 / 0021 (sır sütunları; `Tutar` hücresi; kimlik çifti bildirimi) / 0026 / 0031 (`identity_erase_member` ve `identity_erase_tenant_invites` — beşinci istisna, §6.1 metni) /
0039 / 0045 / 0022 / 0048 / 0002 notları; ADR 0005 risk eki; CLAUDE.md §5'in iki cümlesi ve §4.5(3)'ün farkı) · M16-51 · M16-52 · M16-53 · M16-54 · M16-55 · M16-56 (koşullu) · M16-57 · M16-58 · M16-59 · **M16-60 düştü (K-C2)** · M16-61 ·
M16-62 (taban). ADR 0049 notu: *"YYYY-MM-DD: §2'nin link mekanizması ADR 0051'le yerini aldı; bütçeler, görünürlük ve iki kapı devredildi; 7 günlük bekleme
devralınmadı; metin silinmez"*.

## 9. Kabul notu (2026-11-25, M16-50)

**Karar veren:** kullanıcı, AskUserQuestion, 2026-11-25 — her soruda **önerilen seçenek**. Önerilmeyen cevap olmadığı için plan yeniden yazılmadı (§7'nin kuralı).

- **Q55 = (b)** operatörün işaretlediği inceleme işletmesinde statik QR (`store_review_flags`) · **Q56 = (b)** env `TAPPA_STAFF_PASSWORD_TENANTS` + owner ayarı (var olan işletmeler
  kapalı, yeniler açık doğar) · **Q57 = (a)** tek sayfa `/account-deletion` (çalışan bölümü + owner formu), `ProveOwnership`, ek doğrulama yok · **Q58 = (b)** kütüphanesiz
  Custom Tabs, yedeği sistem tarayıcısı · **Q59 = (b)** aktif + işverenin canlı davetini taşıyan davetli.
- **Beşinci adlandırılmış istisna onaylandı** — dört parça birlikte: CLAUDE.md §4.5(3)'ün *"dört → beş"* farkı ve §2.9.8'deki birebir cümle; ADR 0031 §6.1'in aynı farkı;
  `claudemdidentity_test.go`'nun dizgeleri; `tappa_identitydefiner`'a `UPDATE (email, full_name, cancelled_at) ON admin_invites`. **Uygulama M16-61'de** (CLAUDE.md, §6.1 ve
  test dizgeleri aynı commit'te — bu kabul CLAUDE.md'yi değiştirmez).
- **ADR 0010 istisnası onaylandı** (birebir not ADR 0010'da). **ADR 0005 risk 11 kabul edildi** (birebir metin ADR 0005'te).
- **CLAUDE.md §5'in iki cümlesi onaylandı** (M16-50 kartındaki metin): (1) M16-52 ile, (2) M16-56 ile — kodla aynı commit'te.
- **Notlar düşüldü:** ADR 0049 (yerini aldı), 0010, 0021, 0031, 0002 md. 7, 0026, 0039, 0045, 0022, 0005. **ADR 0048'in notu bu dalda düşülemedi:** dosya
  `origin/ops/tappa-scoped-access` dalındadır, bu dalda yoktur (ölçüldü: `ls docs/adr`); not metni — *"`admin_users.password_hash`'in `tappa_readonly`'ce okunur olması
  adlandırılmış bedeldir; `employee_passwords` ve `employee_password_links` için kalıcı istisna"* — `scoped-access.sql`'in yeniden koşusu kapısıyla o dalda düşülür.
- **Canlı ölçüm** (koordinatör, 2026-11-25; üretim; `kubectl exec` + `psql` ile **`tappa_owner`** olarak — kartın önerdiği `tappa_readonly` değil; `BEGIN READ ONLY … ROLLBACK`;
  kartın dört sorgusu; çıktı yalnız sayı): (1) bir adresin işletme sayısı — n = 1 → 32 adres; **en büyük 1 ≤ K = 3, kapı geçti**; (2) aktif 20 kişi / 3 işletme, davetli 12 kişi
  / 4 işletme; (3) bir işletmedeki en çok aktif owner sayısı **1** — `ownerNotices` FIFO'su (32) ve saatlik özet için geniş pay; (4) kimlik başına bağlı üyelik — n = 1 → 2
  kimlik, n = 2 → 1 kimlik: *"başka bağlı üyelik"* dalı üretimde vardır, [H61-b]'nin fikstürü gerçekçidir.
- **Mağaza sayfaları:** Apple *"Offering account deletion in your app"* ve Google `answer/13327111`, `answer/10787469` 2026-11-25'te okundu (§1.17); bu kabul turunda
  **yeniden açılmadı**. App Review Guidelines'ın öteki maddeleri ana oturumun okumasıdır.
- **Açık kalan kapılar:** **Q13** (saklama süresi) ve hukukçunun **K-A onayı** ile **K-D sorusu** (*"adı olmayan bir kayıt hâlâ delil midir?"*) — M16-55'in owner metni, M16-58 ve
  M16-61 bunları bekler.
