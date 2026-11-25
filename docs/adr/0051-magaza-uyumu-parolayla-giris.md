# ADR 0051 — Mağaza uyumu: çalışanın e-posta + parolayla girişi, uygulamadan işletme kaydı, işletme hesabını silme

- **Durum:** Önerildi (2026-11-25; aynı gün iki denetim turunun NO-GO bulgularıyla iki kez yeniden yazıldı — kimlikler metinde köşeli
  parantezde: *[B-…]* fizibilite/güvenlik, *[A-…]* sıfır hafızalı yürüyüş; ikinci tur *[B2-…]*, *[A2-S…]*) — kabul M16-50'de, kullanıcının.
  Açık sorular **Q55 … Q59**; cevapsız kabul yok, **önerilmeyen bir cevap planı yeniden yazdırır ve yeniden denetletir** (§7). Kabul
  edilince **ADR 0049 §2'nin link mekanizmasının yerini alır** (notu M16-50 düşer — §8).
- **Tarih:** 2026-11-25 · **Dal:** `m16-native-ui` (PR #49, `main` dışı; yalnız belge) · **Plan:** [docs/plan/m16-mobil.md](../plan/m16-mobil.md)
  → *Faz H — Mağaza uyumu* (M16-50 … M16-61)
- **Karar veren:** kullanıcı, 2026-11-25, AskUserQuestion — §2.1'in on bir maddesi (**1–7**, **K-A, K-B, K-C, K-D**) **sabittir**; geri kalan
  **öneridir**.
- **İlgili:** ADR 0002 md. 7 · 0005 · 0010 · 0021 · 0022 (§6, §8, §9, §10) · 0025 · 0026 · 0027 (holding) · 0031 (§5.6, §5.9, §5.10, §6) · 0039 ·
  0045 (§2.3, §2.6, Q40 (a)) · 0048 · **0049** (yerini alır) · CLAUDE.md §4, §5, §6, §7, §9 · Q13, Q23, Q28, Q36, Q40, Q44–Q49.
- **Atıf kuralı:** `file:line` atıfları **`m16-native-ui` `c14cb652`**'ye göredir (kod `828ce9b1`'den beri değişmedi — yalnız belge).
  **Okundu** = dosyadan okundu, çalıştırılmadı. **Ölçülmedi** = doğrulanamadı; ilgili kart ölçer. **Apple ve Google'ın silme sayfaları bu turda
  okundu** (2026-11-25, §1.18); App Review Guidelines'ın öteki maddeleri ana oturumun okumasıdır, M16-50 yeniden okur.

---

## 1. Bağlam — bugün (okundu)

1. **ADR 0049 hiç uygulanmadı:** `git grep -n "signin/request" -- '*.go'` → 0; `/app/v1` yolları `internal/handler/app.go:207-224`.
2. **İlk ekran davet kodudur** (iOS `mobile/ios/Taptime/ActivationCopy.swift:20`; Android `mobile/android/app/src/main/res/values/strings.xml:21`).
   Sonrası lookup → bind → complete (`internal/handler/appactivate.go:15-28`); **lookup plaketsiz** `employeeName`, `businessName`, `locationName`,
   `wifiSsid`, marka döndürür (`appactivate.go:48-59`).
3. **Statik QR aktivasyonu tamamlamaz** (`appactivate.go:249`, `:324-327`; `internal/handler/activate.go:978`). NFC'siz yollar M16-23'tür
   (`main`'de, PR #46): canlı QR, müdür onayı — onay telefonun müdürün karşısında olduğunu varsayar (`internal/invite/approval.go:18-32`).
4. **Davet:** `status = 'active'`'i yalnız `ConsumeInviteAndActivate` yazar (`db/queries/invites.sql:72-97`). `IssueAndDeliver`
   (`internal/invite/manager.go:219`, `:255`) basımda kişinin bütün bekleyen davetlerini iptal eder (kullanıcı kararı 2026-08-08;
   `manager.go:237-254`, `:300-320`; `CancelPendingInvitesForEmployee`, `invites.sql:330-342`). **`consume` (`manager.go:873`) kardeşleri iptal
   etmez.** `IssueParams` yalnız `TenantID`, `EmployeeID`, `TTL`, `OnlyWithoutAddress` taşır (`manager.go:203-216`) — **`Gate` ya da `Origin`
   alanı yoktur**; `INSERT INTO employee_invites` köken yazmaz (`invites.sql:66-71`) *[B2-O2, A2-S16]*.
5. **Davet bütçeleri her satırı sayar** (`CountRecentInvites`, `invites.sql:410-425`); kilit ve sayım yalnız e-posta kanalında
   (`internal/invite/email.go:394-422`). **Davet e-postası işletmenin adını yalnız VIES doğrulanmışsa söyler** (`Recipient.TenantVerified`,
   `email.go:148-155`; `internal/handler/inviteemail.go:48-53`) *[B2-O-d]*.
6. **Parola işi:** bcrypt'i yalnız `internal/adminauth/password.go` ve `internal/operatorauth/{operatorauth,password}.go` içe aktarır (ölçüldü);
   cost 12 (`password.go:33-49`), `Hash` `:180`, `Compare` `:324`, `CompareDummy` `:346`, 72 bayt `:132`, en kısa 8 (`internal/domain/signup/signup.go:130`).
   `WorkGate` dışa açık yöntem taşımaz, yalnız `WithWorkGate` seçeneği (`internal/adminauth/workgate.go:179-273`, `:286-292`); tek kapı
   `TestWorkGateWiring_OneGateServesEveryPasswordService` (`cmd/tappa/workgatewiring_test.go:21`). Prod 1 slot, kuyruk 2 (ADR 0039). Panel
   girişi 9 karşılaştırma (`internal/adminauth/manager.go:163`); başarısız giriş bütçesi ve kodun hesabı *"10 × 8 × ~380 ms ≈ 30 s"*, kimlik yolunda
   9 ile ~34 sn (`internal/handler/adminratelimit.go:655-681`); hesap izi kapatmaz (`:762-803`); **`TryCharge` dersi** (`signupconfirm.go:586-594`).
   Anahtar türetme emsali: `deriveInviteKey` `SessionHMACKey`'den etiketli HMAC (`internal/adminauth/admininvite.go:136-142`, kullanım `:252`).
7. **Oturumsuz kimlik kanıtı hazır değil:** `AuthenticatePerson` oturum açar (`identity.go:129-136`); `proveIdentityPassword` bir `SignupClaim`'e
   bağlı (`signupconfirm.go:583-597`); `RevokeAll` çerez oturumu ister (`identityops.go:196-206`).
8. **`employees.email`:** tekillik `00037`'nin kısmi indeksi (`(tenant_id, email) WHERE email IS NOT NULL AND status <> 'deactivated'`,
   `00037_*.sql:96-105`). **Adres her yazımda değişebilir:** `SetEmployeeEmail` yalnız `email`'i yazar, davetleri aynı transaction'da
   `CancelPendingInvitesForEmployee` emekliye ayırır (`db/queries/employees.sql:1007-1030`); `employees`'te başka `UPDATE`'ler de vardır
   (`:764`, `:845`, `:1068`, `:1180`, `:1382`) *[B2-H2]*. Sıralama dersi: `ORDER BY tenant_id` rastgele, en eski önce şart
   (`00017_*.sql:30-60`); aynı dosya en eski önce sıralamanın doygunluk sayısını **deterministik** bir kehanete çevirdiğini de yazar (`:85-95`).
   `tappa_resolver`'ın bugünkü yetkileri: `employee_invites (id, tenant_id, employee_id, code_hash, expires_at, used_at)` (`00009:242-243`) +
   `(cancelled_at)` (`00012:87`); `admin_users (id, tenant_id, email, password_hash, status)` (`00011:212-213`) + `created_at` (`00017:346`) +
   `identity_id, unlinked_at` (`00071:129`) + `role` (`00081:94`); `tenants.id` (`00051:155`).
9. **`tenants`:** `vat_number NOT NULL UNIQUE`, `business_type` ve `structure` `NOT NULL`, varsayılansız (`00001_create_tenants.sql:15-23`); Go
   tarafı `VatNumber string` (`internal/store/models.go:568`, `tenants.sql.go:32`) — **NULL'u okuyamaz** *[B2-rollback]*. `tappa_app`'ın `INSERT`'i
   sütun düzeyi (`00016:85-89`). Operatörün yaratma yolu yok (`internal/db/operatorpool.go:496-706`). **ADR 0021'in *"ASLA"* sütunu**
   `tappa_opdefiner`'a `tenants`'ta INSERT/DELETE/TRUNCATE vermez (`docs/adr/0021-*.md:162-170`); 00063'ün bir kerelik kontrolü
   (`00063_*.sql:296-307`), `privCatalogAllowed` (`internal/db/privcatalog_test.go:283`) ve `opDefinerTenantsUpd63` (`internal/db/holdingops_test.go:743`)
   pinler. **`admin_invites` yalnız `manager`** (`00077:154`) ve `invited_by` zorunlu bir `admin_users` FK'sidir (`00077:168-169`) — yeni işletmenin
   ilk owner'ı buradan doğamaz. Kayıt taslağı `admin_signup_claims` (`00078:123`), `tappa_app`'tan geri alınmış (`:158`), fonksiyonları
   `tappa_identitydefiner`'ın (`identity_open_signup_claim` `:212`, `identity_claim_signup` `:257`, sahiplik `:438-440`) *[B2-H3]*. Askı
   nedenleri `'non_payment','terms_breach','security_hold','customer_request'` (`00063:354`, `:962`); `op_record_vat_check` boş numarayı
   reddeder (`00034:869`). KDV'si doğrulanmamış işletmenin aktivasyon ailesinde logo çizilmez (`internal/domain/tenant/activationbrand.go:84`).
10. **Kayıt sihirbazı:** KDV zorunlu ve biçimi denetlenir (`internal/domain/signup/signup.go:319-351`); işletme e-postadaki onayla doğar
    (`signupconfirm.go:23-44`); `SignupDone` giriş düğmesi verir (`:562-571`).
11. **Kabuk bağlantıları:** `publicchrome.templ:170`, `:180`, `:186`, `:197-205`, `:213`, `:277` (*Built by*), `:281`, `:292`; sihirbazın kendi
    giriş bağlantıları `signup.templ:290` (*"Go to sign in"*) ve `:840` (*"Sign in"*); yasal gövde `legal.templ:158-163`; taslakta
    `https://taptime.mt/legal/cookies` (`privacy-draft.md:68`) ve `mailto:`.
12. **App Link:** yalnız `/t`, `/q` (`internal/handler/applinks.go:70`).
13. **Ingress:** erişim log'u kapalı tek Ingress `tappa-panel` (`/admin`, `/oauth` — `deploy/k8s/40-ingress.yaml:226-259`), pin
    `TestPackaging_ThePanelPathsAreRoutedWithoutAnAccessLog` (`cmd/tappa/panelingress_test.go:45`). **`main`'e her deploy `40-ingress.yaml`'ı
    uygular** (`.github/workflows/deploy.yml:934-935`) — **birleştirme = Ingress değişikliği**, ayrı onay yoktur *[B2-O1]*.
14. **Kesici** (`internal/mail/breaker.go:29-44`, `:76-78`, `:112-119`) ve owner bildirimleri (`internal/handler/ownernotice.go:20-38`, `:60-74`).
15. **Çalışanın silme talebi** dalda (`786bd004`, `13f24033`, `cca8bad8`; `internal/domain/tenant/staffdeletion.go:3-25`).
16. **Log taraması:** `password` `R7_TRIGGERS`'ta (`scripts/redline-check.sh:2382`), `e_?mail` R7M'de (`:2642`); R7A (`:2589-2593`) `app*.go`.
17. **Holding sitesinin ekleme-yalnız kayıtları** çalışan adını taşır: `site_punches.employee_name` (`00046_*.sql:130`), satır ve TRUNCATE
    tetikleyicileri her rol için (`:178-186`); `punch_events.employee_ref`, `employee_code`, `location_name` (`00048_*.sql:151-156`, `:277`).
    `punch_claims` (`00068:134`) ve `transaction_reviews` (`00005:305`) da ekleme-yalnızdır. Yedekler 30 günde düşer
    (`scripts/pg-backup-ship.sh:40`, `RETENTION_DAYS`). `admin_identities.email` `NOT NULL` (`00069:168`) *[B2-H4, K-D]*.
18. **Mağaza kuralları — bu turda okundu (2026-11-25):**
    - Apple *"Offering account deletion in your app"*: *"Offer to delete the entire account record, along with associated personal data … only
      offering to temporarily deactivate or disable an account is insufficient."* · *"Apps not operating in highly regulated industries should not
      require people to make a phone call, send an email, use a chat support flow, or go through other support flows."* · *"Inform the person how
      long it will take to delete the account and provide a confirmation when the deletion is complete."* · *"You may add steps to verify the identity
      of the person making the request, and to confirm that they want to delete the account"* · *"If you're legally required to retain some user
      information, let people know what information will and won't be retained."*
    - Google *"Understanding Google Play's app account deletion requirements"* (`support.google.com/googleplay/android-developer/answer/13327111`):
      *"provide a web link resource where users can request app account deletion and associated data deletion"*, *"The weblink must be functional"*,
      *"delete the user data associated with that app account"*, *"retain certain data for legitimate reasons such as security, fraud prevention or
      regulatory compliance"* + *"must clearly inform users about your data retention practices"*.
    - Ana oturumun okuması (M16-50 yeniden okur): 2.1(a), 3.1.1, 3.1.3(c), 3.2 (forum retleri, topluluk kaynağı), 4.8.

## 2. Karar

### 2.1 Kullanıcı kararları (2026-11-25) — sabit

1. İlk ekran **"Sign in"** + **"Create a business account"** (web `/signup`, uygulama içi tarayıcıda). 2. Çalışan girişi **e-posta + parola**;
ADR 0049'un linkinin yerini alır. 3. Cihaz bağı ilk plaket / QR dokunuşunda; parola tek başına bağlamaz. 4. Uygulama içi kayıt "uygulama
modu"nda biter; fiyata, panele, Billing'e yol yok. 5. İşletme silme owner'ın uygulama içi talebi; kayıtlar kalır. 6. Demo *"Taptime Demo"*;
inceleyici demo çalışanın e-posta + parolasını yalnız konsollarda alır. 7. Önce Play iç testi; üretim PR #49'u, hukukçu onaylı metni,
`TAPPA_APP_ANDROID_SIGNER_SHA256`'yı bekler.
8. **K-A** — silme **30 gün** içinde tamamlanır; sayfa *"within 30 days"*; operatör askıya alır ve owner'ların + çalışanların kişisel verisini
   anonimleştirir; giriş-çıkış ve fatura kayıtları yasal saklama için kalır, sayfa söyler; tamamlanınca owner'a onay e-postası; **hukukçu onayı +
   Q13 kapıdır**.
9. **K-B** — *"Built by"* (`publicchrome.templ:277`) uygulama modunda kalır.
10. **K-C** — demo işletme **operatör yüzeyinden, KDV'siz**; `/signup`'a sahte KDV girmez.
11. **K-D** — silme tamamlanınca **holding sitesinin ekleme-yalnız kayıtlarındaki çalışan adları kalır** (`site_punches.employee_name`;
    `punch_events.employee_ref`, `employee_code`, `location_name`) — ekleme-yalnız kuralı bozulmaz. Sayfa ve onay e-postası, holding sitelerinin
    giriş-çıkış kayıtlarının adı yasal saklama süresince tuttuğunu ve **yedeklerin onu 30 gün içinde düşürdüğünü** söyler. Hukukçuya açık soru:
    *"adı olmayan bir kayıt hâlâ delil midir?"* (§4.3'ün gerekçesi).

### 2.2 Kimlik bilgisi: çalışan satırı başına, adrese bağlı bir parola özeti

- **`employee_passwords`** (00118): `tenant_id`, `employee_id`, `digest`, **`set_email citext NOT NULL`** (parolanın belirlendiği adres), `set_at`;
  `UNIQUE (tenant_id, employee_id)`; §6 beşlisi. **Parola yalnız `set_email = employees.email` iken geçerlidir** — çözümleyicinin `JOIN`'i bunu
  ister (`OPERATOR(public.=)`). Adres hangi yoldan değişirse değişsin (panel, API, içe aktarma — `employees.sql`'in beş `UPDATE`'i) eski parola
  kendiliğinden **etkisizdir**; tetikleyici ya da her yazma yoluna ek kod gerekmez. Saldırı *"kendi işletmende kendi adresinle parola koy, bağla,
  sonra adresi kurbanınkine çevir"* — adres değişince parola düşer *[B2-H2]*. Etkisiz satır kişisel veri sayılır ve silme yollarında silinir.
- `tappa_app`'a `SELECT, INSERT, UPDATE (digest, set_email, set_at)` ve **`DELETE`** (RLS altında; yalnız çalışanın silme talebi — §2.9.1 — ve
  `/staff/password`'ün yeniden belirlemesi kullanır) *[B2-H1]*.
- Kod `internal/adminauth`'ta (`StaffPasswords`, `WithWorkGate`; wiring testi genişler). Kural: panelinki + **gömülü ret listesi** — seçim kuralı
  *[A2-S11]*: (1) yayımlanmış bir listeden en çok 10 000 giriş, **lisansı kamuya açık bir depoyla uyumlu** (CC0 / MIT / OGL — kaynağın URL'si,
  lisansı ve dosyanın SHA-256'sı dosya başında); (2) böyle bir liste bulunmazsa elle yazılmış ≤ 300 kalıp (klavye yürüyüşleri, tekrarlar,
  `taptime`, `password` türevleri, yıl ekleri). Liste sır değildir; parola **değeri** taşımaz, yaygın parola **sözlüğüdür**.
- Telefonda parola yok; girişin döndürdüğü aktivasyon kodu tamamlanana / süresi dolana dek güvenli depoda.

### 2.3 İlk parola ve sıfırlama: tek akış

1. `POST /app/v1/password/request` — 202, `[250, 300] ms`, eşleşmeyene e-posta yok, kaynak kovası 429.
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
   deseni, etiket `staff-password-link`) *[B2-LOW]*; ömür 1 sa; kişi başına tek canlı link; çıkış kutusu; bütçeler
   `LockTenantForStaffPasswordLimits` altında.
5. **Sayfa** `GET/POST /staff/password`: GET token'ı kısa ömürlü bir çereze alır ve **token'sız** temiz adrese 303 verir; her cevap
   `Referrer-Policy: no-referrer` (`adminreset.go:37`, `accountemail.go:487` emsali) *[B2-O1]*; POST tek transaction'da: özet (`set_email` =
   güncel adres), link `used_at`, öteki linkler `revoked_at`, **kişinin bekleyen `origin='password'` davetleri** — yeni sorgu
   **`CancelPendingPasswordInvitesForEmployee`** (`WHERE origin = 'password'`) — ve kilit: **`LockTenantForInviteLimits`** (parolalı giriş basımıyla
   aynı kilit → eşzamanlı bir basım ya önce biter ve iptal edilir ya sonra gelir ve yeni özeti görür) *[A2-A18]*. Kişiye *"password set"*.
6. **Bildirimler** `personNotices` (kişiye; kuyruk 32, `SendExempt`, kişi + tür başına günde 5). **Bütün yeni e-postalar `TenantVerified`
   kuralına uyar:** işletmenin adı yalnız KDV'si VIES doğrulanmışsa yazılır, aksi hâlde nötr metin (oltalama yolu) *[B2-O-d]*. KDV'siz demo
   işletmenin adı hiçbir e-postada görünmez (bedel 16).

### 2.4 Giriş: parola → aktivasyon daveti → ilk dokunuşta cihaz

1. Sıra: kaynak tavanı → `TryCharge` bütçeleri → `WorkGate` → aday çözümleyicisi → tam **K = 3** karşılaştırma → karar.
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
   `app_signin_revoked_at`'i temizlenir (aynı transaction).
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

### 2.6 Uygulama içi kayıt: "uygulama modu"

- `…/signup?client=app` (iOS `SFSafariViewController`, Android Q58). Bayrak yalnız sunumdur.
- **Kaybolan:** `publicchrome.templ:170`, `:180` (bağlantısız marka), `:186`, `:197-205`, `:213`, `:281`; **sihirbazın `signup.templ:290` *"Go to
  sign in"* ve `:840` *"Sign in"* bağlantıları** *[A2-S10]*. **Kalan:** `:277` (K-B), `:292` + yasal gövdenin iç bağlantıları (`?client=app`),
  dış bağlantılar, `mailto:`, çapalar.
- **`/account-deletion?client=app` de uygulama modundadır** ve aynı kapalı `href` testine girer *[A2-S12]*.
- Son ekranlar *"your business is ready — open the dashboard on a computer"*; inceleyici KDV adımında durabilir.

### 2.7 İnceleyici bağı — Q55

Öneri (b): operatörün işaretlediği işletmede statik QR bağlar (`tenants.store_review_at`, yalnız `op_set_store_review`); (a) manager girişi;
(c) canlı QR; (d) reddedildi.

### 2.8 Açılış — Q56, ve birleştirmede ne canlıya çıkar

Öneri (b): env `TAPPA_STAFF_PASSWORD_TENANTS` + owner ayarı (eski kapalı, yeni ve operatörün açtığı açık; `signup.Provision` aynı transaction'da
`UPDATE` ile). **Birleştirmede env'den bağımsız canlıya çıkan** *[B2-removal]*: (1) bütün migration'lar (yeni tablolar, sütunlar, çözümleyiciler,
`vat_number`'ın `NULL` olabilmesi); (2) Ingress'in yeni erişim log'suz yolları; (3) `CountRecentInvites`'in daralması ve tüketimdeki kardeş iptali
(**her işletmede** davranış: tüketim artık kalan davetleri iptal eder); (4) panelin owner ayarı ve operatörün *"New business"* formu; (5)
`/account-deletion`'ın çalışan bölümü. **Env'e bağlı:** `/app/v1/password/request`, `/app/v1/signin`, `/staff/password`. **Ayrı bayrağa bağlı:**
`/account-deletion`'ın owner formu (§2.9.3). KF/KM: 401 ve 404 *"Have an invitation code?"* metniyle; mağaza üretimi KF/KM owner kararını bekler.

### 2.9 İşletme hesabını silme — K-A, K-D; giriş yeri Q57

1. **Çalışanın kendi talebi de parolayı siler** *[B2-H1]*: M16-37'nin `RequestDeletion`'ı (`staffdeletion.go`) aynı transaction'da kişinin
   `employee_passwords` ve `employee_password_links` satırlarını siler ve `employees.app_signin_revoked_at`'i yazar — çalışan hesabı = parola
   kimliği; geri dönmek yeni bir **işveren** davetinin tüketimini ister (§2.4.5). Deaktive kişide de aynı (parola dokunuşta kullanılmaz; T59'un
   *"kimlik bilgisi iptal edilmez"* kuralı cihaz ve oturum içindir).
2. **Tek sayfa, iki bölüm:** `https://taptime.mt/account-deletion` (Play'in tek URL'si) — çalışan yolu + owner formu.
3. **Owner formu bayrak arkasında** *[B2-H/O-a, A2-S26, B2-O6]*: env `TAPPA_BUSINESS_DELETION` (boş = form kapalı; sayfa yalnız çalışan bölümünü ve
   owner için *"available soon"* çizer, bcrypt yapan uç bağlanmaz). Bayrak **M16-61 üretimde çalışana dek açılmaz**; mağaza üretim gönderimi bayrağı
   bekler. **Faz H PR'ı Q13 ve hukukçu beklerken birleşebilir** (bayrak kapalı, M16-61'in kodu yoksa da). Bayrak açıkken form panel girişiyle aynı
   `WorkGate`'i paylaşır — dağıtık bir saldırı panel girişini 503'e itebilir (bedel; panel girişine öncelik verilmez — kapı tek, ADR 0039).
4. **Kim / kanıt:** aktif owner (askıdaki işletmeninki de); `adminauth.Manager.ProveOwnership` (oturumsuz, `AuthenticatePerson`'ın işi ve
   zamanlaması). **Çok adımlı durum** *[B2-O-e]*: adım 1 e-posta + parola (POST) → adım 2 işletme seçimi + adını yazma (POST) → bitti. Adımlar
   arasında **yalnız** HMAC'li kısa ömürlü bir çerez taşınır: `{kanıtın kimliği (identity ya da admin_users id), izinli tenant id'leri, son geçerlilik
   10 dk}` (`signupstate.go`'nun imzalı durum deseni); her formda senkronizasyon token'ı çerez + alan (`adminreset.go`'nun deseni); Origin kontrolü.
   **Asla loglanmaz:** e-posta, parola, çerez değeri, token.
5. **Talep — tek transaction:** `deletion_requested_at`, **`deletion_requested_by`** (owner'ın `admin_users.id`'si — onay e-postasının alıcısı buradan
   çözülür) *[A2-S20]*, `tenant.deletion_requested` audit, isteyenin bu işletmedeki panel oturumları (`RevokeMembershipSessions`). Sonra isteyene
   ve öteki owner'lara e-posta (`TenantVerified` kuralıyla).
6. **Tek yönlü:** `BEFORE UPDATE OF deletion_requested_at, deletion_requested_by` tetikleyicisi; dolu değeri yalnız `tappa_opdefiner` sahipli kod
   değiştirebilir — **tetikleyicinin `current_user` karşılaştırması `tappa_opdefiner`'ın sahibi olduğu her `op_*`'ı geçirir; asıl kapı
   `GRANT`'tır:** `tappa_opdefiner`'a bu iki sütunda `UPDATE` yalnız `op_cancel_business_deletion` ve `op_complete_business_deletion`'ın ihtiyacı
   için verilir ve `privCatalogAllowed` pinler *[B2-O8]*.
7. **Tamamlama envanteri — M16-55'in metin onayından ÖNCE sabitlenir** *[B2-H4, A2-S22]*. Değer kuralı: **sütun NULL olabiliyorsa `NULL`; `NOT
   NULL` ya da bir CHECK varsa yer tutucu** — e-posta `deleted-<satır id>@deleted.invalid` (RFC 2606 `.invalid`; ASCII CHECK'ini geçer), ad
   `Deleted person`.

   | Tablo / sütun | Tamamlamada |
   |---|---|
   | `employees` — ad, ad/soyad, e-posta, çalışan kodu, dış kimlik (kesin liste şemadan) | NULL ya da yer tutucu |
   | `employee_passwords`, `employee_password_links` | satırlar silinir |
   | `sessions` — oturumlar iptal; `device_info` | NULL |
   | `app_devices` — cihazlar iptal; `device_label` | **kalır** — sunucunun platformdan koyduğu *"iPhone"/"Android"* (`appactivate.go:36-37`), kişisel değil |
   | `admin_users` — ad, e-posta | yer tutucu (e-posta `NOT NULL` ise); oturumlar iptal |
   | `admin_identities` — e-posta (`NOT NULL`, `00069:168`), özet | yalnız başka aktif üyeliği yoksa: yer tutucu + özet geçersiz kılınır; aksi hâlde yalnız bu üyelik kapanır |
   | `admin_invites` — `email`, `full_name` | yer tutucu (`00077`'nin CHECK'leri) |
   | `admin_signup_claims` — `email`, `draft`, `password_hash` (bu işletmeyi doğuran taslak) | satır `tappa_identitydefiner`'ın fonksiyonuyla silinir |
   | `punch_claims.note`, `transaction_reviews.note` (serbest metin) | **kalır** — ikisi de ekleme-yalnız (`00068:134`, `00005:305`); hukukçu sorusuna eklenir |
   | `site_punches.employee_name`; `punch_events.employee_ref`, `employee_code`, `location_name` | **kalır** (K-D) |
   | `transactions`, `audit_log`, `billing_periods`, `tags`, `tenants` satırı | kalır (fatura ve kayıt) |
   | yedekler | 30 gün içinde düşer (`pg-backup-ship.sh:40`) — sayfa söyler (K-D) |

   Askı nedeni **`customer_request`** (`00063:354`).
8. **Sayfa metni** (K-A + K-D; hukukçu onaylı): süre (30 gün), silinen kişisel veri, kalan kayıtlar ve süresi (Q13), holding sitelerinin kayıtlarının
   adı yasal süre boyunca tutması, yedeklerin 30 günü, onay e-postası. Apple'ın *"let people know what information will and won't be retained"* ve
   Google'ın *"clearly inform users about your data retention practices"* şartları bu metinle karşılanır.
9. **Operatör tarafı:** liste `op_read_deletion_requests(p_session, p_ticket)` (yeni, M16-55'in migration'ında; en eski önce, kalan gün)
   *[A2-S13]*; tamamlama `op_complete_business_deletion`; onay e-postası operatör paketinden **tüketici tarafı bir arayüzle** (`DeletionNotifier`,
   `internal/handler/operator`'da tanımlı; uygulaması `internal/handler`'ın `personNotices`'ı; `cmd/tappa` bağlar) — operatör paketi `internal/mail`'i
   ve `internal/handler`'ı içe aktarmaz *[A2-S21]*.

### 2.10 Demo işletme operatörden — K-C (M16-60)

**Seçilen yol: operatörün bastığı tek kullanımlık bir *owner kaydı* (KDV'siz kayıt taslağı), sahibi `signup.Provision`'la tam `/signup`'taki gibi
doğar** *[B2-H3]*. Reddedilen: `op_create_business`'ın `tenants`'a doğrudan yazması — ADR 0021'in *"ASLA"* sütununu (`tenants`'ta INSERT) kırar
ve ilk owner'ı doğuracak bir yol yoktur (`admin_invites` yalnız `manager`, `invited_by` zorunlu FK — §1.9).

- **Akış:** operatör *"New business"* formu (ad, saat dilimi, `business_type` = formdan, varsayılan **`other`**; `structure` = **`single`**; owner adresi) →
  `op_mint_business_claim(p_session, p_name, p_timezone, p_business_type, p_structure, p_owner_email)` (`tappa_opdefiner`; `operator_audit_log`) → bu
  fonksiyon **`identity_open_operator_claim(p_token_hash, p_email, p_draft)`**'i çağırır (yeni; sahibi `tappa_identitydefiner`; taslak `kind =
  'operator'`, `vat_number` yok, parola özeti yok) → link operatör ekranında **bir kez** gösterilir (TOTP'li oturum; operatör paketi posta göndermez —
  link sahibine operatörün kendi kanalıyla gider; demo için sahibi kullanıcıdır) → sahibi linki açar, `/admin/signup/confirm`'ün `kind = 'operator'`
  dalı **yeni bir parola seçtirir** (yeniden girme değil) → `signup.Provision` işletmeyi, kimliği ve owner üyeliğini **`tappa_app` ile, bugünkü
  transaction'da** doğurur: `vat_number = NULL`, `created_by_operator = true`, `staff_password_signin = true` (Q56 (b)).
- **ADR 0021 / 0031 eki** (M16-50 yazar): `tappa_opdefiner` **tek** bir kimlik fonksiyonunda (`identity_open_operator_claim`) `EXECUTE` alır — definer'dan
  definer'a tek çağrı, ADR 0031 §6'nın kapalı listesine bir satır. `tenants`'taki *"ASLA"* sütunu **değişmez** (operatör `tenants`'a yazmaz).
  `00063:296-307`'nin kontrolü uygulanmış bir migration'ın bir kerelik kontrolüdür, **değişmez**; `privCatalogAllowed` yeni `EXECUTE` satırını ve
  `tappa_opdefiner`'ın yeni `SELECT` sütunlarını (operatör ayrıntısının gösterdikleri) alır; `opDefinerTenantsUpd63`'nin premisi §2.9.6'nın iki
  `UPDATE` sütunuyla **değişir** — M16-55 testi günceller (`holdingops_test.go:743`).
- **Şema** (M16-60'ın migration'ı): `tenants.created_by_operator boolean NOT NULL DEFAULT false` + `GRANT INSERT (created_by_operator)` (`00016`'nın
  sütun listesine); `ALTER COLUMN vat_number DROP NOT NULL`; `CHECK (vat_number IS NOT NULL OR created_by_operator)` — **aynı dosyada, doğrulanmış**
  (`tenants` küçüktür, `lock_timeout = '1s'`; `NOT VALID` + ayrı `VALIDATE` yalnız sıcak tablolarda) *[A2-S8, B2-LOW]*; `admin_signup_claims.kind`.
  `/signup`'ın yolu `created_by_operator`'ı asla yazmaz (test).
- **KDV'siz işletmenin bedelleri:** aktivasyon ailesinde logo yok (`activationbrand.go:84`) · e-postalarda ad yok (§2.3.6) · operatörün KDV yeniden
  denetimi reddedilir (`op_record_vat_check`, `00034:869`) — ekran düğmeyi gizler · fatura ve operatör ekranları *"No VAT number (opened by Taptime)"*
  çizer *[A2-S9]*.
- **Geri alma tehlikesi** *[B2-rollback]*: eski ikili `VatNumber string` ile `NULL`'u okuyamaz (`models.go:568`) — **M16-60'ın ilk kullanımından sonra kod
  geri alınamaz, yalnız ileri düzeltilir**; geri alma haritasına girer.

### 2.11 Sevk, migration sırası, taban ölçümü, geri alma

- **Kod kartları PR #49'u bekler** *[A2-S2]*: M16-51, M16-52, M16-55, M16-56, M16-60, M16-61 kodu PR #49 `main`'e girdikten sonra `main`'den açılan
  **`m16-store`** dalında yazılır (belge kartı M16-50 beklemez); Faz H PR'ı o daldır.
- **Migration numaraları yürütme sırasıyla, commit anında verilir** *[A2-S5]*: goose `-allow-missing` olmadan koşar (`deploy/README.md:3085`; `00067:70`),
  yani uygulanmış en yüksek numaranın altına sonradan dosya giremez. Öneri sıra: **00118, 00119, 00120** M16-51 → **00121** M16-60 → **00122** M16-55
  (`deletion_requested_at`, `deletion_requested_by`, `deletion_completed_at` sütunları, tetikleyici, `op_cancel_business_deletion`,
  `op_read_deletion_requests`, `op_read_tenant_detail`'in yeni tanımı — `created_by_operator` ve silme sütunlarını gösterir). **M16-61 ve M16-56
  numaralarını kendi commit'lerinde, o anki ilk serbest numaradan alır**; koşullu M16-56 seçilmezse numara hiç yazılmaz — boşluk oluşmaz. Her biri
  `op_read_tenant_detail`'i ancak yeni bir sütun gösterecekse yeniden tanımlar (`store_review_at` → M16-56'nın).
- **`Down` kuralları** *[A2-S14]*: 00122 dolu `deletion_requested_at` varken reddeder, `op_*`'ların önceki tanımlarını birebir geri koyar; M16-61'inki
  dolu `deletion_completed_at` varken reddeder (anonimleşmiş veri geri gelmez); M16-56'nınki dolu `store_review_at` varken reddeder.
- **`scoped-access.sql`** (ADR 0048, `origin/ops/tappa-scoped-access:deploy/db/scoped-access.sql:135-148`): tabloya düzey `GRANT SELECT ON ALL
  TABLES`; yalnız `tags` sütun listesiyle. Yeni tablolar için **sütun istisnası** — `employee_passwords.digest` ve `employee_password_links`'in
  token özeti `tappa_readonly`'ye verilmez; dosyanın yeniden koşusu bir kapıdır *[B2-O-f]*.
- **Taban ölçümü Faz H birleştirmesinden ÖNCE** (log'lar 15 gün) *[B2-O6]*; SigNoz eşiği M16-59'un ölçtüğü tabana bağlanır *[A2-S18]*.
- **Geri alma haritası:** config PR → `git revert`; yasal yayım → önceki sürüm; **kod geri alma M16-60'ın ilk kullanımından sonra yasak** (yukarıda);
  owner formu → `TAPPA_BUSINESS_DELETION` boşaltılır.
- **3.2 yedeği:** hazır Resolution Center cevabı + kapı (itiraz / Unlisted App Distribution / Apple Business Manager).

## 3. Güvenlik değerlendirmesi

| Tehdit | Bu ADR'yle | Kalan |
|---|---|---|
| Parola doldurma / dağıtık tahmin | bütçeler (`TryCharge`), ret listesi, K iş, WorkGate, SigNoz | küresel tavan; plaketsiz sızıntı yalnız `businessName` + `employeeName` (parola davetinde `locationName`/`wifiSsid` yok — §2.4.6) |
| Adres değiştirerek ele geçirme (B2-H2) | parola `set_email`'e, link `link_email`'e bağlı | yok — [H51-n] |
| İşverenin eski davetiyle ikinci bağ | tüketimde kardeş iptali | yok — [H52-c] |
| Saldırgan işletmelerle pencere dışına itme | en eski önce, süzgeçler ve yönetici dışlaması `LIMIT`'ten önce, parolasız satır pencereye girmez | > 3 işletmeli kişi en yeni işletmesinde link alamaz |
| Doygunluk kehaneti | sayı yalnız doğru parolayla görünür | — |
| Parola paylaşımı | bağ plakette, Q40 (a), e-postalar | risk 1 aynen |
| Posta kutusu ele geçirme | bekleyen parola davetleri iptal, kişiye ve owner'a e-posta, kurtarma yolu | adres bir doğrulayıcıdır |
| Oltalama (doğrulanmamış işletme adı) | `TenantVerified` kuralı | — |
| Silme talebinin kötüye kullanımı | aktif owner, kanıt, ad yazdırma, e-postalar, 30 gün içinde operatör iptali | tek owner'ın talebi işletmeyi siler (K-A) |
| Silme formunun WorkGate'i doldurması | form bayrakla kapalı doğar | bayrak açıkken panel girişi 503 alabilir |
| Ingress log'unda token | `/staff`, `/account-deletion` erişim log'suz; GET token'ı adres çubuğundan siler; `no-referrer` | nginx hata log'u |
| `tappa_readonly` ile özet okuma | `scoped-access.sql` sütun istisnası | dosya yeniden koşulmazsa yeni tablo hiç görünmez (güvenli taraf) |

## 4. Bedeller

1. Yeni kimlik bilgisi türü; ret listesi sızmış parola servisinin yerini tutmaz. 2. Adres bir doğrulayıcıdır. 3. Holding çalışanı işletme başına parola;
adres değişince parola yeniden belirlenir. 4. Çıkış kutusu dayanıklı değil. 5. WorkGate'in takası genişler. 6. Mekân NAT'ı. 7. Bellekteki kovalar
pod sayısıyla. 8. Uygulama modu sunum sözleşmesi. 9. Silme 30 gün sürer. 10. Q55 (b)'nin istisnası. 11. İnceleyici KDV adımında durur. 12. Davetli
kişi 24 saatte tamamlamazsa yeni işveren daveti. 13. Owner-çalışan yönetici adresiyle parola kullanamaz. 14. Operatöre e-posta yok, liste her iş günü.
15. KDV'siz işletme: logo yok, ad e-postalarda yok, KDV denetimi yok. 16. Demo işletmenin adı hiçbir e-postada görünmez. 17. Parola davetinde Wi-Fi
ipucu yok. 18. Çalışanın silme talebinden sonra parolalı giriş yeni bir işveren daveti ister. 19. Holding kayıtlarında ad kalır (K-D). 20. M16-60'tan
sonra kod geri alınamaz. 21. Demo owner'ının linki operatörün kendi kanalıyla gider.

## 5. Kırmızı çizgiler

- **§4.1, §4.2:** değişmez. **§4.3:** `transactions`, `site_punches`, `punch_events`, `punch_claims`, `transaction_reviews` hiçbir yolda değişmez;
  anonimleştirme yalnız ekleme-yalnız olmayan kişi sütunlarında (K-D); hukukçu sorusu §2.1(11). **§4.4:** plaket bağı `sun.Verify`. **§4.5:** yeni
  tablolar §6 beşlisiyle; operatör yalnız `op_*`; tek definer-definer çağrısı adlandırılmış (§2.10). **§4.6:** silme talebi personeli durdurmaz.
  **§4.7 + §7:** parola, adres, token, kod, çerez değeri hiçbir log / hata / `detail`'de yok; yeni sır yok (anahtar türetilir).

## 6. Mağaza ve gizlilik

App Store *Contact Info → Email Address*; Play *Personal info → Email address*; Play'in tek silme URL'si `/account-deletion`; gizlilik bölüm 12:
parola, işletme kaydı, işletme silme (30 gün, silinen, kalan — K-D dahil — yedekler, onay e-postası).

## 7. Karar verilmedi

**Q55 … Q59** (öneriler var; önerilmeyen cevap ⇒ yeniden yazım + yeniden denetim, M16-51 açılmaz). Hukukçuya iki soru: K-A'nın saklama süresi (Q13)
ve K-D'nin *"adsız kayıt delil midir?"* sorusu. *Sonra:* tarayıcıda parolayla giriş, *Change password*, panelden *"Send app sign-in e-mail"*, operatör
linkinin e-postayla gönderimi, iOS `webcredentials:`.

## 8. Uygulama

M16-50 kabul + ADR 0049 / 0010 / 0021 / 0026 / 0031 / 0039 / 0045 / 0022 notları + ADR 0005 risk eki · M16-51 (00118–00120) · M16-52 · M16-53 ·
M16-54 · M16-55 (00122) · M16-56 (koşullu, sırası gelince) · M16-57 · M16-58 · M16-59 · M16-60 (00121) · M16-61 (sırası gelince). ADR 0049 notunun metni:
*"YYYY-MM-DD: §2'nin link mekanizması ADR 0051'le yerini aldı; §2.4 bütçeleri parola linkine, §2.6 görünürlüğü `origin='password'`'e, §2.7'nin iki
kapısı ADR 0051 §2.8'e devredildi; 7 günlük bekleme devralınmadı; metin silinmez"*. CLAUDE.md §5 cümlelerinin metnini M16-50'de kullanıcı onaylar.
