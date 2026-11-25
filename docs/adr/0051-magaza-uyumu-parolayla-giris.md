# ADR 0051 — Mağaza uyumu: çalışanın e-posta + parolayla girişi, uygulamadan işletme kaydı, işletme hesabını silme talebi

- **Durum:** Önerildi (2026-11-25) — kabul M16-50'de, kullanıcının. Açık sorular **Q55 … Q59**
  ([open-questions.md](../plan/open-questions.md)); cevapsız kabul yok. Kabul edilince **ADR 0049 §2'nin link mekanizmasının
  yerini alır** (ADR 0049'a tarihli *"yerini aldı"* notunu M16-50 düşer; bu belge yalnız tarif eder — §8).
- **Tarih:** 2026-11-25 · **Dal:** `m16-native-ui` (PR #49, `main` dışı; yalnız belge) · **Plan:**
  [docs/plan/m16-mobil.md](../plan/m16-mobil.md) → *Faz H — Mağaza uyumu* (M16-50 … M16-59)
- **Karar veren:** kullanıcı (ürün sahibi), 2026-11-25, AskUserQuestion — §2.1'in yedi maddesi **sabittir**, bu belge onları
  yeniden açmaz. Geri kalan her şey (§2.2 … §2.9) **öneridir**; açık kalanlar Q55 … Q59'dadır.
- **İlgili:** ADR 0002 md. 7 (`tappa_resolver`) · ADR 0005 (kabul edilen riskler) · ADR 0010 (deaktivasyon) · ADR 0021
  (`op_*`) · ADR 0022 (§6 çıkış kutusu, §8 şablon, §9 oran, §10 log) · ADR 0025 (askı) · ADR 0026 (aktivasyon) · ADR 0031
  (panel kimliği, §5.6 giriş, §5.9 sıfırlama) · ADR 0039 (bcrypt kapısı) · ADR 0045 (§2.3 cihaz anahtarı, §2.6 NFC'siz
  aktivasyon, Q40 (a)) · **ADR 0049** (yerini alır) · CLAUDE.md §4, §5 *"Aktivasyon dokunuşu"*, §6, §7, §9 · open-questions
  Q13, Q23, Q36, Q40, Q44–Q49.
- **Atıf kuralı:** `file:line` atıfları **`m16-native-ui` `828ce9b1`**'e göredir. **Okundu** = dosyadan okundu, çalıştırılmadı.
  **Ölçülmedi** = bu belgeyi yazan doğrulayamadı; ilgili kart ölçer. Apple ve Google metinleri ana oturumun 2026-11-25
  okumasıdır (aşağıda tırnak içinde); bu belgeyi yazan sayfaları **yeniden açmadı** — M16-50 güncel metni okur.

---

## 1. Bağlam — bugün (okundu)

1. **ADR 0049 hiç uygulanmadı.** `git grep -n "signin/request" -- '*.go'` → **0** (ölçüldü, `828ce9b1`). `/app/v1`'in yolları
   `internal/handler/app.go:207-224`'tür: activate (lookup/bind/complete/approval), nonce, tap, me, revoke, claims, hours,
   account/delete — giriş yolu yok. M16-34/35/36 `todo`'dur (state.md ledger).
2. **Uygulamanın ilk ekranı davet kodudur:** *"Paste the activation link your workplace sent you."*
   (`mobile/ios/Taptime/ActivationCopy.swift:20`; Android `activation/Inputs.kt:12`). Sonrası ADR 0026'nın üç adımıdır —
   lookup (harcamaz) → bind (onay + açık anahtar, harcamaz) → complete (ilk gerçek NFC; tek transaction'da davet tüketilir,
   kişinin tarayıcı oturumları ve eski cihazları iptal edilir, cihaz doğar) (`internal/handler/appactivate.go:15-28`).
3. **Statik QR aktivasyonu tamamlamaz.** Uygulama: *"NFC only — a QR read carries no proof of a touch"*
   (`appactivate.go:249`), `!p.HasSUN()` → `appErrActivationNeedsTouch` (`:324-327`); tarayıcı aynı kural
   (`internal/handler/activate.go:978`, `CompleteByTap` `:948`). Statik QR = SUN'suz `/t` → `ChannelQR`
   (`internal/sun/params.go:196`). NFC'siz iki yol M16-23'tür (bu dalda): canlı QR (`complete.liveQR`, `appactivate.go:228-230`,
   `:313-315`) ve müdür onayı (`invite.Manager.ActivateDeviceApproved`, `internal/invite/approval.go:317`; migration 00115).
   state.md'nin 2026-11-18 ölçümü aynı: *"statik QR aktivasyonu tamamlamaz (yalnız NFC; NFC'siz yollar M16-23)"*.
4. **"Aktive" ⇒ "bir davet tüketildi".** `employees.status = 'active'`'i yazan **tek** ifade `ConsumeInviteAndActivate`'tir;
   tüketim ve durum geçişi bilerek kaynaştırılmıştır (`db/queries/invites.sql:72-97`). Davet yaratmanın tek dışa açık yolu
   `IssueAndDeliver`'dır (`internal/invite/manager.go:219`, `:255`); tüketim `consume`'dadır (`:873`); uygulama yarısı
   `ActivateDevice` (`:805`). Davet ömrü `minTTL` 1 sa … `maxTTL` 30 gün, varsayılan 7 gün (`manager.go:133-135`). Kanal
   arayüzü tek yöntemdir (`internal/invite/channel.go:68-70`); linki çağırana veren kanal emsali `ManagerVisibleChannel`'dır
   (`channel.go:98-105`, panel modu).
5. **Parola işi bugün yalnız panelde ve tek kapıdadır.** bcrypt cost 12 (`internal/adminauth/password.go:33-49`, gerekçe
   ölçülü), `Hash` `:180`, `Compare` `:324`, `CompareDummy` `:346`, 72 bayt sınırı (`MaxPasswordBytes`, `:132`); en kısa
   parola 8 karakter (`internal/domain/signup/signup.go:130`, `MinPasswordRunes`; operatörünki 14 —
   `internal/operatorauth/password.go:31`). Süreç geneli **tek** `WorkGate` (`internal/adminauth/workgate.go:113`, `:128`;
   ADR 0039: prod'da **1 slot**, bekleme 10 sn, kaynak başına bir global yer). Panel girişi her denemede `1 + MaxCandidates`
   = 9 karşılaştırma öder (`internal/adminauth/manager.go:163`; ADR 0031 §5.6). Bütçeler: kaynak başına başarısız giriş 10 /
   10 dk (`internal/handler/adminratelimit.go:655-681`, `adminAttemptLimit`), hesap başına 10 / 10 dk — **hiçbir şeyi
   kapatmaz, yalnız audit'i sınırlar** (`adminratelimit.go:762-803`; kilit = gerçek kullanıcıya karşı DoS). Sıfırlama linki
   1 saat (`internal/adminauth/reset.go:119`); sıfırlama isteğinin cevabı 250 ms tabanlı (`internal/handler/adminresetlimits.go:347`)
   ve `[250, 300] ms` bandında (`internal/handler/adminresetoutbox.go:44-49`).
6. **`employees.email` tenant içinde tekildir, global değil** (`db/migrations/00003_*.sql:48-53`, `:80-84`: *"ayni kisi iki
   farkli isletmede calisan olabilir"*). Adresle tenant'lar arası çözüm emsali `00011`'in `OPERATOR(public.=)` dersidir
   (`db/migrations/00011_add_admin_resolution.sql:288-330`). `tappa_resolver` `tenants.id`'yi `00051`'den beri okur
   (`db/migrations/00051_*.sql:155`) — dokunulmaz (ADR 0049 §3 son satırının dersi).
7. **Kayıt sihirbazı (`/signup`) — brief'in bir iddiası ölçüyle düzeltildi.** Üç adım (`internal/handler/signup.go:31-33`:
   `/signup`, `/signup/places`, `/signup/account`); son POST işletme **yaratmaz**, bir taslak açar ve adrese bir onay linki
   e-postalar (*"Check your inbox"*, `/signup/sent`); işletme, kimlik ve owner üyeliği yalnız o link **onaylanınca** doğar —
   `POST /admin/signup/confirm` (`internal/handler/signupconfirm.go:15-27`). Onaydan sonra **panel kendiliğinden açılmaz**:
   `SignupDone` sayfası *"Sign in to your dashboard"* düğmesiyle giriş sayfasına gönderir (`signupconfirm.go:562-571`,
   `SignInHref: adminLoginPath`; `web/templates/pages/signup.templ:697-698`). Brief *"after signup the panel opens in the
   same browser"* diyordu — **ölçülen bu değildir**; doğru olan: onay linki e-postadan açılır, yani sihirbazı başlatan
   tarayıcıda değil e-posta istemcisinin açtığı tarayıcıda biter. **Ödeme adımı yoktur** (ölçüldü: `signup.templ`'de fiyat /
   deneme / abonelik sözcüğü yok; tek eşleşme *"As it appears on your invoices."* `:313`); faturalama panelin Billing
   bölümünde aylık faturadır (`internal/handler/billing.go`, M6-12).
8. **Sihirbazın ve yasal sayfaların kabuğu fiyatlandırmaya götürür (ölçüldü).** Sihirbaz `PublicChrome`'u çizer
   (`signup.templ:46`, `SignInPrompt`, `OwnRoots`); kabuğun başlığı `LandingNav`'ı (*Product · Security · **Pricing*** —
   `web/templates/pages/landingview.go:466-470`) ve *Sign in*'i (`publicchrome.templ:186`, `:197-202`), telefon menüsü ve
   altbilgi `PublicProductLinks`'i (*Pricing* dahil — `landingview.go:502-509`; `publicchrome.templ:213`, `:281`) taşır.
   Yasal sayfalar da aynı kabuktadır, *Start free* ile (`web/templates/pages/legal.templ:63`). **Bugünkü uygulama bu
   sayfalara zaten link verir:** iOS onay ekranı `legal/privacy` (`mobile/ios/Taptime/ActivationViews.swift:302`), Android
   şartlar sayfası `/legal/privacy` ve `/legal/terms` (`mobile/android/app/src/main/kotlin/mt/taptime/app/MainActivity.kt:319`)
   — yani uygulamadan *Pricing*'e bugün **bir dokunuşluk** bir yol vardır (§2.6 kapatır).
9. **Universal Link / App Link yalnız iki yolu açar:** `appLinkPaths = []string{TapPath, liveqr.LiveQRTapPath}`
   (`internal/handler/applinks.go:70`; canlı AASA `/t`, `/q` — `docs/store/app-store.md:21`). `/signup`, `/activate`,
   `/admin/*` uygulamayı açmaz.
10. **Çalışanın silme talebi bugün vardır** (M16-37, bu dalda): talep + her yerde çıkış + audit + owner e-postası; kayıtlar
    kalır, deaktive kişinin kimlik bilgisi iptal edilmez (`internal/domain/tenant/staffdeletion.go:3-25`, `RequestDeletion`
    `:78`); owner'lara e-posta istek yolu dışında, `Breaker.SendExempt` ile (`internal/handler/ownernotice.go:3-25`). Operatör
    işletmeyi askıya alabilir (`op_suspend_tenant`, `db/migrations/00063_*.sql`; `internal/db/operator.go:1697`) ve
    işletmeleri okur (`op_read_tenants`, `00029`; `op_read_tenant_detail`, `00063`).
11. **Mağaza belgeleri bugünü anlatır:** App Store etiketi *"Contact info … No"* (`docs/store/app-store.md:63`), inceleyiciye
    davet linki (`:68-75`); Play *"E-mail … No"* (`docs/store/google-play.md:110`), *"Accounts: created by the employer outside
    the app"* (`:115`); gizlilik taslağı bölüm 12 *"the app uses no password and no sign-in cookie"* (`docs/legal/privacy-draft.md:150`
    paragrafı) ve e-postayla giriş köşeli parantezi (`:160`).
12. **Neden şimdi** (ana oturumun okuması, 2026-11-25): App Review Guidelines **2.1(a)** — *"include demo account info (and turn
    on your back-end service!) if your app includes a login"*; *"Before You Submit"*: *"provide either an active demo account or
    fully-featured demo mode, plus any other hardware or resources that might be needed to review your app (such as login
    credentials or a sample QR code)"*. **5.1.1(v)** — *"If your app supports account creation, you must also offer account
    deletion within the app."* (uygulama içi kayıt **zorunlu değil**). **4.8** istisnası (özet, **doğrula**): mevcut kurumsal
    hesapla ve şirketin kendi hesap sistemiyle giriş yapan iş uygulamaları Sign in with Apple istemez. **3.1.3(c)** —
    *"only sold directly by you to organizations or groups for their employees"*. **3.1.1** (ABD dışı) — *"buttons, external
    links, or other calls to action that direct customers to purchasing mechanisms other than in-app purchase"* yasak. **3.2**
    retleri (Developer Forums 740245, 755917, 704518, 126794 — **topluluk, resmî değil**): *"Business apps with limited
    audiences should generally be distributed using Apple Business Manager."* Yalnız girişli B2B SaaS'a 3.1.1 reddi: 811018
    (Aralık 2025, çözümsüz); 724032 (Ocak 2023) otel SaaS'ı itirazla 3 günde onaylandı. Piyasa: Connecteam şirketi mobil
    uygulamadan yaratır; When I Work çalışanı *"log in with your email address and password"*. Google Play: *App content →
    App access*'te inceleyici giriş bilgisi ve talimat; uygulama içi kayıt şartı yok; *Data safety*'nin silme talebi
    **uygulamasız çalışan bir web URL'si** ister.

## 2. Karar

### 2.1 Kullanıcı kararları (2026-11-25) — sabit

1. **"Sign up" = işletme kaydı.** Uygulamanın ilk ekranı **"Sign in"** ve **"Create a business account"** sunar; ikincisi var
   olan web `/signup`'ı **uygulama içi tarayıcıda** açar.
2. **Çalışan girişi = e-posta + parola** (yeni bir kimlik bilgisi). ADR 0049'un tek kullanımlık e-posta linkinin **yerini
   alır** (M16-34/35/36 uygulanmadı — §1.1).
3. **Cihaz bağı:** parola kimliği kanıtlar; telefon işverene **ilk plaket / QR dokunuşunda** bağlanır. ADR 0026'nın kuralı
   korunur: **parola tek başına hiçbir cihazı bağlamaz.**
4. **Uygulama içi kayıt "uygulama modu"nda biter:** son ekran *"işletmen hazır — paneli bir bilgisayarda aç"*; uygulamanın
   içinden fiyatlandırmaya, panele ya da Billing'e **yol yoktur** (3.1.1).
5. **İşletme hesabını silme** (uygulama işletme yaratabildiği için 5.1.1(v)): owner'ın uygulama içi **"Delete my business
   account" TALEBİ** — oturumlar kapanır, talep platform operatörüne ulaşır, **kayıtlar kalır** (CLAUDE.md §4.3; çalışan
   talebinin deseni, `staffdeletion.go`).
6. **Demo işletme "Taptime Demo"** üretimde `/signup`'tan açılabilir (sonra, kullanıcı; parolaları kullanıcı yazar, depoya
   asla). İnceleyici bir demo **çalışanın** e-postası + parolasını **yalnız** App Store Connect / Play Console'da alır.
7. (Önceden, geçerli) Önce Play iç testi; üretim PR #49'un `main`'e girmesini, hukukçu onaylı gizlilik metnini ve
   `TAPPA_APP_ANDROID_SIGNER_SHA256`'yı bekler; mağaza ekran görüntüleri yalnız demo işletmeden.

### 2.2 Kimlik bilgisi: çalışan satırı başına bir parola özeti

- **Tablo `employee_passwords`** (öneri ad; migration **00118**, M16-51): `tenant_id`, `employee_id`, `digest text NOT NULL`,
  `set_at timestamptz NOT NULL`; `UNIQUE (tenant_id, employee_id)`; CLAUDE.md §6'nın beşlisi (`tenant_id NOT NULL`, ENABLE +
  FORCE RLS, `NULLIF(current_setting('app.tenant_id', true), '')::uuid` politikası, `tenant_id` önde indeks, GRANT). `tappa_app`'a
  `SELECT, INSERT` ve **yalnız** `UPDATE (digest, set_at)`; `DELETE` yok. Özet yalnız `adminauth.Hash` ile (cost 12 — panelin
  ölçülmüş sayısı, `password.go:33-49`); karşılaştırma `adminauth.Compare` / `CompareDummy`. Q03'ün *"bcrypt, bu dosya o
  kararın tamamı"* sözü (`password.go:28-30`) korunur: ikinci bir KDF yok, ikinci bir özet biçimi yok.
- **Parola kuralı panelinkidir:** en az `signup.MinPasswordRunes` (8) karakter, en çok `adminauth.MaxPasswordBytes` (72 bayt) —
  üretici reddeder, karşılaştırıcı 72'den sonrasını atar (`password.go:109-123`). Aynı ret metni.
- **Neden satır başına, global kimlik değil:** ADR 0031'in global kimliği CLAUDE.md §4.5'in **bilinçli istisnasıdır** (beş
  tenant kapsamsız tablo, definer'lar). Çalışanın bağı zaten **tenant başınadır** (plaket bir işletmenindir), satırın sahibi
  işverendir (deaktivasyon, ADR 0010) ve aynı kişi iki işletmede iki satırdır (§1.6). Tenant kapsamsız bir *çalışan kimliği*
  dördüncü bir istisna olurdu — **reddedildi** (bu ADR'nin kapsamında). Bedeli: holding çalışanı her işletmesinde ayrı parola
  belirler (§2.4'ün seçicisi aynı parolayı iki satırda tanır).
- **Telefonda parola yoktur.** Uygulama parolayı ne saklar ne loglar; sistem parola yöneticisinin önerisi için alanlar
  `username` / `password` içerik türüyle işaretlenir (saklayan sistemdir, biz değil). iOS `webcredentials:` ilişkili alanı
  *Sonra*'dır (AASA'ya yeni bölüm — §1.9'un tek listesi değişmez).

### 2.3 İlk parola ve sıfırlama: tek akış, e-postadaki link

**İlk parola ile unutulan parola aynı akıştır** — piyasa deseni (davet e-postası → parola belirle → uygulamada giriş) ve tek
bir kehanetsiz uç.

1. **İstek — uygulamada.** *"Forgot password? / First time here?"* → adres → `POST /app/v1/password/request` (imzasız, gövde
   `{"email": string}`; katı). **Her durumda 202**, boş gövde, `no-store`, süre `[250 ms, 300 ms]` bandında (reset'in bandı,
   §1.5). Tek istisna kaynak kovası (429 + `Retry-After`, adresten bağımsız). Eşleşmeyen adres **hiçbir iz bırakmaz ve hiç
   e-posta almaz** (ADR 0049 §2.2'nin gerekçesi aynen: tanımadığımız adrese posta = posta bombası aracı).
2. **Kim alır** (sırayla; her ret sessiz): adres §2.8'in izin verdiği, **askıda olmayan** bir işletmede (a) **aktif** bir
   çalışanın ya da (b) **işverenin canlı bir davetini taşıyan davetli** (`invited`) bir çalışanın — **Q59**'un önerisi; (b)
   yeni çalışanın ilk yoludur ve işverenin *"bu kişi gelsin"* kararını taşır; davetsiz `invited` satır ve deaktive kişi hiçbir
   şey almaz. Adres aynı işletmede bir yönetici adresi değildir (`ErrAddressIsAdministrators` deseni). Çözüm
   `tappa_resolver` sahipli dar bir SECURITY DEFINER fonksiyonudur (`00011` deseni: `SET search_path = pg_catalog, pg_temp`,
   `OPERATOR(public.=)`, en çok **K** satır, yalnız kimlikler — özet yok); sonrası her işletmenin **kendi** `WithTenant`'ında,
   art arda (ADR 0031 §6: bir istek asla iki tenant'ta **aynı anda** koşmaz).
3. **Link.** Tablo `employee_password_links` (00118, §6 beşlisi): 256 bit token, yalnız HMAC'i; **ömür 1 saat**
   (`adminauth.ResetTTL`'in sayısı — istek kişinin kendisinden, az önce geldi); **kişi başına tek canlı link** (ADR 0031
   §5.9'un EM-7C kuralı: canlı link varken hiçbir şey basılmaz, iptal edilmez, gönderilmez). Gönderim istek yolunda değil,
   `adminresetoutbox.go` biçiminde sınırlı bir **çıkış kutusundan** (FIFO 32, tek işçi; kapanış boşaltmasına reset çıkış
   kutusunun yanında girer). Holding: adres birden çok işletmede uygunsa işletme başına bir link, bir e-posta.
4. **Sayfa — web, uygulama değil.** Link `https://taptime.mt/staff/password?t=…` (öneri yol) açar: GET token'ı kısa ömürlü bir
   çereze alır ve temiz adrese 303 verir (`adminreset.go`'nun `readLink` deseni; **GET tüketmez** — posta tarayıcılarının ön
   yüklemesi, ADR 0031 R19); POST yeni parolayı (iki kez) alır, `adminauth.Hash`'i **WorkGate içinde** koşar (ADR 0039'un
   kapsamına beşinci servis olarak girer), özeti yazar, kişinin öteki canlı linklerini iptal eder. Sayfa *"Password set. Go
   back to the Taptime app and sign in."* der; **panel, fiyat, kayıt linki yoktur**; kabuk §2.6'nın uygulama modu
   kabuğudur. `/staff/password` **App Link / Universal Link'e eklenmez** (§1.9'un listesi aynen) — link tarayıcıda açılır,
   uygulamaya derin bağ yoktur (Q40'ın kuralının ruhu).
5. **Haber.** Parola belirlenince kişinin kendi kutusuna *"Your Taptime password was set"* e-postası (`passwordnotice.go`
   deseni, `Breaker.SendExempt`) — istemediği bir değişikliği kutunun sahibi öğrenir. **Cihazlar ve oturumlar iptal edilmez**
   (parola bir cihaz değildir; bağ ancak bir dokunuşla değişir — §2.4).
6. **Panelden "Send app sign-in e-mail"** (işveren tetikli aynı link) *Sonra*'dır; davet e-postasının şablonu bu ADR'de
   değişmez (ADR 0022 §8).

### 2.4 Giriş: parola → aktivasyon daveti → ilk dokunuşta cihaz

`POST /app/v1/signin` (imzasız — henüz cihaz yok), gövde `{"email": string, "password": string}` (katı).

1. **Sıra:** kaynak taşma tavanı → kaynak deneme bütçesi → **WorkGate** (adres okunmadan önce — ADR 0039 §2'nin zamanlama
   kuralı) → çözümleyici (§2.3.2'nin uygunluğuyla; yalnız uygun satırların özeti döner) → **tam K karşılaştırma**, eksikler
   `CompareDummy` ile doldurulur → karar.
2. **K = 3** (öneri ad `StaffMaxCandidates`): bir adresin bu yolda tanınabileceği işletme sayısı. Her giriş **K** karşılaştırma
   öder (≈ 3 × 0,38 sn, `password.go:47-49`'un ölçüsüyle hesap — prod'da **ölçülmedi**); panelin 9'unun üçte biri. Canlıda
   bir adresi taşıyan aktif işletme sayısının en büyüğü **ölçülmedi** — M16-50 ölçer; > 3 ise kart durur ve sorar.
3. **Tek başarısızlık cevabı:** bilinmeyen adres · yanlış parola · deaktive kişi · ayarı kapalı ya da askıdaki işletme ·
   uygun olmayan durum — hepsi aynı `401 {"error":"signin_failed"}`, aynı iş (çözümleyici uygun olmayanı **karşılaştırmadan
   önce** eler, kalan dolgu sabittir). Beş durumun düzlüğü ADR 0031 §5.6'nın beş durumlu zamanlama testinin biçimiyle ölçülür
   (bilinen adreste audit yazımı ölçülen yolun **içindedir**).
4. **Başarı bir oturum ya da cihaz DEĞİLDİR.** Doğrulanan her satır için o işletmenin bağlamında, `IssueAndDeliver`'dan,
   **`origin = 'password'`** (00118'in yeni `employee_invites.origin` sütunu, `CHECK (origin IN ('employer','password'))`)
   ve **ömrü `minTTL` = 1 saat** olan bir aktivasyon daveti basılır; kod **cevapta** döner (linki çağırana veren kanal —
   `ManagerVisibleChannel`'ın biçimi, e-posta yok). Cevap `200 {"workplaces":[{"code":…, "businessName":…}]}`; birden çoksa
   uygulama *"Which workplace?"* seçicisini gösterir. Sonrası **bugünkü** lookup → bind → complete'tir (§1.2): onay ekranı,
   açık anahtar, ilk gerçek dokunuş, tek transaction'da tüketim + Q40 (a) iptalleri + cihaz. Böylece **§1.4'ün değişmezi
   korunur** — *"aktive" ⇒ "bir davet tüketildi"* — ve ADR 0045 §2.3 değişmez: **uygulamada taşıyıcı token yoktur**; parola
   yalnız bir sonraki bağ için bir davet üretir.
5. **Parola daveti işverenin davetini emekliye ayırmaz.** Basım yalnız kişinin önceki `origin='password'` davetlerini iptal
   eder; işverenin canlı daveti (ADR 0049 §2.1.3'ün korunan şeyi) yaşamaya devam eder. Kilit ve sıra M16-52'nin işidir
   (`LockTenantForInviteLimits`'in altında).
6. **Tamamlanma ADR 0026'nındır, değişmez:** plaket (atomik `ctr`, §4.4) · canlı QR (M16-23) · müdür onayı (M16-23) — ve
   yalnız **Q55 (b)** seçilirse inceleme işletmesinde statik QR (§2.7). Bu dokunuş `transactions` satırı **yazmaz**.
7. **Görünürlük — ADR 0049 §2.6 aynen devralınır, `self` yerine `password`:** `consume` `Activation.Origin`'i döndürür; her
   tüketen yol (tarayıcı, uygulama, canlı QR, müdür onayı) tüketim commit edince tek bildirim fonksiyonunu çağırır: owner'lara
   `ownerNotices` (`kind = password_activation`) **ve** kişinin kendi kutusuna *"A new phone was set up with your Taptime
   password"* (ikisi de `SendExempt`, istek yolunda değil); panelde çalışan kartı *"Set up by app sign-in on <date>"* ve M6-11
   anomali ekranında son 30 gün; statik bir test yeni bir tüketen çağıranın bildirimi atlamasını yakalar. **T59 (a)**
   (`tap.signed_out_phone` audit satırı, yalnız tarayıcı yolu, yalnız parolayla girişi açık işletmelerde) ADR 0049 §2.6'daki
   biçimiyle devralınır.
8. **Kilit yok** (panelin kararı, §1.5): hesap başına sayaç **audit'i sınırlar, girişi kapatmaz** — bir yabancının gerçek
   çalışanı dışarıda bırakması DoS olurdu. Parola tek başına hiçbir şey bağlamadığı için (§2.1.3) uzaktan tahminin ödülü bir
   davettir, kayıt değil; bağ işverenin plaketinde fiziksel dokunuş ister (§3).

### 2.5 Bütçeler

| Kova | Değer | Nerede | Gerekçe |
|---|---|---|---|
| giriş — kaynak, başarısız | 10 / 10 dk | bellek | `adminAttemptLimit`'in sayısı |
| giriş — kaynak, karşılaştırmaya ulaşan her deneme | 30 / 10 dk | bellek | 30 × K × 0,38 sn ≈ 34 sn CPU / 10 dk — panelin başarısız giriş bütçesinin hesabıyla aynı pay (10 × 9 × 0,38 sn ≈ 34 sn, `adminratelimit.go:655-681`); panelin `adminLoginWorkLimit`'i (106 giriş / 10 dk, 960 karşılaştırmalık modelden türetilir — `:683-759`) daha geniştir, personel girişi seyrek (yeni telefon) olduğu için dar tutulur; mekân NAT'ında 30 kişi aynı 10 dakikada giriş yapmaz (bedel 6) — hesap, ölçülmedi |
| giriş — hesap başına audit | 10 / 10 dk, aşınca tek `FirstOverLimit` satırı | bellek | `adminAccountLimit`'in kuralı: **kapatmaz** |
| süreç | `WorkGate` (paylaşılan) | bellek | ADR 0039 — beşinci servis |
| link isteği — kaynak | 5 / 10 dk, 20 / gün | bellek | ADR 0049 §2.4 |
| link isteği — süreç alt bütçesi | 100 gönderim / sa | bellek | süreç kesicisinin (300/sa) üçte biri |
| link isteği — işletme | 10 / sa, 30 / gün | DB, kiracı kilidi altında | ADR 0049 §2.4 |
| link isteği — kişi | 1 / sa, 3 / gün | DB | tek canlı link kuralıyla zaten 1 / sa |
| parola sayfası — link başına geçersiz form | 10 / 10 dk | bellek | `signupClaimLinkLimit`'in biçimi (`signupconfirm.go:74-83`) |

Bellekteki kovalar pod sayısıyla çarpılır (bedel 7). WorkGate doluyken giriş, link sayfası ve panel girişi **birlikte** 503
alır; tap etkilenmez (ADR 0039 §5'in takası genişler — adlandırılmış bedel).

### 2.6 Uygulama içi kayıt: "uygulama modu"

- **Giriş:** uygulama `https://taptime.mt/signup?client=app`'i uygulama içi tarayıcıda açar — iOS'ta `SFSafariViewController`
  (SafariServices, sistem çerçevesi — bağımlılık değil), Android'de **Q58**'in seçtiği yol (öneri: kütüphanesiz Custom Tabs
  niyeti, yoksa sistem tarayıcısı).
- **Bayrak yalnız sunumdur, hiçbir şeyi gevşetmez.** `client=app` sihirbazın imzalı durumuna (`signupstate.go`) ve taslağa
  yazılır; **değiştirdiği tek şey çizilen bağlantılardır.** Origin kontrolü, senkronizasyon token'ı, bal küpü, en kısa doldurma
  süresi, dört bütçe, VIES, onay linki — bayt bayt aynıdır. Bayrağı bir masaüstü tarayıcısında elle koyan kişi **daha az
  bağlantılı** bir sayfa görür; o kadar. Uygulama bayrağı unutursa sonuç bir mağaza riskidir, güvenlik açığı değil (uygulama
  tarafında URL'yi pinleyen test). User-agent okunmaz (ADR 0004 §8'in ruhu).
- **Uygulama modunda kabuk:** başlıkta yalnız marka (bağlantısız); `LandingNav` yok, *Sign in* ve *Already with Taptime?* yok,
  *Start free* yok, telefon menüsü ve altbilgi ürün bağlantıları yok (§1.8). Yasal bağlantılar kalır ama `?client=app` taşır ve
  **yasal sayfa da** aynı kabukla çizilir — uygulamanın kendi gizlilik / şartlar linkleri de `?client=app` ile açılır (§1.8'in
  bugünkü bir dokunuşluk yolu kapanır). **Ölçü:** uygulama modundaki her sayfanın `href` kümesi kapalı bir listedir —
  `/signup*`, `/legal/*?client=app`, sayfa içi çapalar.
- **Son ekranlar:** sihirbazın uygulamada görünen sonu `/signup/sent`'tir; uygulama modunda metni (anlamı): *"Check your inbox
  and open the link to finish. Then your business is ready — open the dashboard at taptime.mt on a computer."* (adres **düz
  metin**, bağlantı değil). Onay linki e-posta istemcisinin tarayıcısında açılır (uygulama dışında); taslak `client=app`'i
  taşıdığı için `SignupDone` da aynı dili konuşur: *"Your business is ready. Open the dashboard on a computer: taptime.mt"* —
  **"Sign in to your dashboard" düğmesi yok** (kullanıcı kararı 4'ün harfi: uygulamada başlayan akış panele yol göstermez).
  Metinler §9'un korumalı ekranları değildir, ama Faz G ortak kuralı (2) gereği AskUserQuestion + mockup ile onaylanır.
- **Universal Link / App Link:** `/signup*`, `/admin/signup/confirm`, `/staff/password` `appLinkPaths`'e **girmez**
  (`applinks.go:70` aynen); bir test listeyi bu üç yolun **dışında** pinler.

### 2.7 İnceleyici plaketsiz nasıl bağlanır — Q55

İnceleyicinin elinde bir demo çalışanın e-postası + parolası ve basılabilir bir statik QR (demo plaketinin, `sun.PlaqueQRURL`)
vardır; **plaket yoktur.** Bugün statik QR bağlamaz (§1.3). Seçenekler Q55'tedir; **öneri (b)**:

- **(b) Operatörün işaretlediği inceleme işletmesinde statik QR bağlar.** `tenants.store_review_at timestamptz` (migration
  **00121**, koşullu) yalnız yeni bir `op_set_store_review(tenant, on)` SECURITY DEFINER fonksiyonuyla yazılır (ADR 0021:
  `tappa_operator` çağırır, her çağrı `operator_audit_log`'a; `tappa_app`'a `UPDATE` **verilmez** — işletme kendini
  işaretleyemez). Bağ adımının *"NFC only"* kuralı (`appactivate.go:249`, `activate.go:978`) yalnız şu koşulda statik QR'ı
  kabul eder: davetin işletmesi **ve** plaketin işletmesi aynı, ikisi de `store_review_at IS NOT NULL`, plaket aktif ve monte.
  Koruma yalnız davetin tek ifadeli tüketimidir (canlı QR varyantıyla aynı şekil — ADR 0045 §2.3). Sonraki statik QR dokunuşu
  bugünkü gibi `flag`'dir (`base:qr-requires-ip`); inceleyici gerçek FLAGGED kaşesini görür (Faz G kullanıcı kararı 3).
  KF/KM ve işaretsiz her işletme **bayt bayt bugünkü gibi** — bir parite testi.
- (a) Q47 (a)'nın cevabı korunur: inceleyiciye ayrıca bir **manager** panel girişi verilir, kendisi M16-23'ün onayını verir
  (kod değişikliği yok; M16-39'un adlandırdığı manager yetkileri bedeli + iki cihazlı akış).
- (c) Canlı QR (M16-20): bir ekranın eşlenmesi panel girişi ister → (a)'nın bedeli + ekranın açık tutulması.
- (d) Kullanıcının nöbetle onayı — Q47'de reddedildi (inceleme saati belirsiz).

### 2.8 Hangi işletmelerde açık — Q56

**Öneri (Q56 (b)):** env **`TAPPA_STAFF_PASSWORD_TENANTS`** — boş = iki uç (`/app/v1/signin`, `/app/v1/password/request`) ve
`/staff/password` **hiç bağlanmaz** (bilinmeyen `/app/v1` yolunun cevabı, bayt bayt); virgülle ayrılmış UUID'ler = yalnız
onlar; `*` = hepsi; bozuk değer açılışı reddeder. `deploy/k8s/05-config.yaml`'a boş girer; ilk dolum yalnız demo UUID'siyle
ayrı bir config PR'ıdır, `*`'a geçiş ikinci bir config PR'ı (ikisi de kullanıcının — birleştirme = deploy). İkinci kapı
owner'ın ayarı `tenants.staff_password_signin boolean NOT NULL DEFAULT false` (00118): **var olan** işletmeler kapalı doğar
(KF/KM'nin güvenlik sınırı sessizce değişmez); **bu özellikten sonra kayıt olan** işletmeler açık doğar (`signup.Provision`
yazar) — uygulamadan işletme açan biri personelinin uygulamada giriş yapabilmesini bir ayar aramadan bekler. Reddedilen:
herkes kapalı (yeni işletmenin personeli ayar bulunana kadar giremez), herkes açık (KF/KM'nin sınırı onaysız değişir).

### 2.9 İşletme hesabını silme talebi — Q57

**Öneri (Q57 (a) + (1)):**

- **Giriş yeri:** uygulamanın oturumsuz ilk ekranının altında ve Ayarlar'da **"Delete a business account"** → uygulama içi
  tarayıcıda `https://taptime.mt/business/delete?client=app` (öneri yol). Sayfa **uygulamasız da çalışır** — Play'in silme
  web URL'si de odur. Kabuk §2.6'nın kabuğudur: panel menüsü, Billing, fiyat yok.
- **Kim:** işletmenin **aktif owner**'ı. Sayfa panel kimliğinin e-postası + parolasını ister; kanıt
  `SignupClaims.ProveWithIdentityPassword`'ün deseniyle alınır (karşılaştırma uygulamada, **panel oturumu açılmaz** —
  `signupconfirm.go:583-597`, `proveIdentityPassword`); panel girişinin bütçelerini ve WorkGate'i paylaşır, tek başarısızlık
  cevabı (ADR 0031 §5.6). Kimlik birden çok işletmenin owner'ıysa kanıttan sonra işletme seçilir; owner olmadığı işletme
  listede yoktur. Son adım işletme adını yazdırır (yanlışlıkla gönderime karşı) ve ne olacağını söyler.
- **Ne olur — tek transaction, o işletmenin bağlamında:** `tenants.deletion_requested_at` (migration **00120**; `tappa_app`'a
  `UPDATE (deletion_requested_at)` yalnız `IS NULL`'dan dolmaya — tetikleyici ya da yüklemle tek yönlü) + `audit_log`
  `tenant.deletion_requested` (aktör owner'ın `admin_users` satırı; `detail`'de adres yok) + **isteyen owner'ın bu işletmedeki
  panel oturumları kapanır**. Commit'ten sonra: öteki owner'lara `ownerNotices` (`kind = business_deletion_request`,
  `SendExempt`) ve operatöre bildirim (operatörün var olan bildirim yolu — `internal/handler/operator` `opnotice_test.go`'nun
  kapsadığı yol; ayrıntısı **ölçülmedi**, M16-55 ölçer). Operatör yüzeyi talebi `op_read_tenants` / `op_read_tenant_detail`'in
  yeni bir sütunuyla görür (00120 fonksiyonları yeniden tanımlar — uygulanmış migration değişmez, CLAUDE.md §3).
- **Ne olmaz:** işletme **çalışmaya devam eder** — personelin oturumları ve cihazları kapanmaz (kapanırsa tek bir eş-owner'ın
  tıklaması kayıtları sessizce durdururdu — §4.6'nın ruhu); `transactions`, `audit_log`, `billing_periods` (dondurulmuş aylar,
  `0016`), `employees`, `tags` (plaket satırı asla silinmez) **silinmez**. Sonrası operatörün elle işidir: owner'la doğrular,
  işletmeyi askıya alır (ADR 0025, `op_suspend_tenant`), veriyi sözleşme ve hukukun dediği gibi işler (Q13 anonimleştirme,
  Q23 DPA). Talep geri alınabilir: owner operatöre yazar (bu ADR yeni bir geri alma ucu açmaz).
- **Tekrar:** dolu `deletion_requested_at`'te ikinci talep **yazmaz** ve bildirimi tekrarlamaz; aynı cevap (çalışan talebinin
  `Repeat` kuralının ruhu, `staffdeletion.go:18-25`).

## 3. Güvenlik değerlendirmesi

| Tehdit | Bugün | Bu ADR'yle | Kalan |
|---|---|---|---|
| **Parola doldurma / kaba kuvvet** (sızmış parola listeleri) | yol yok | kaynak bütçeleri (§2.5), WorkGate, K sabit karşılaştırma; **ödül yalnız bir aktivasyon davetidir** — bağ işverenin plaketinde fiziksel dokunuş, canlı QR'ın ~20 sn'lik ekranı ya da müdür onayı ister (§2.4.6) | dağıtık kaynaklı tek hesap denemesi yalnız audit'e düşer (kilit yok — bilinçli, §2.4.8); doğru tahmin + işyerine erişimi olan biri = aşağıdaki satır |
| **Parola paylaşımı = buddy punching** | telefon devri (ADR 0005 risk 1, A4) | arkadaşın parolasıyla giren kişi yine plakette dokunmalıdır ve **bağ gerçek çalışanın cihazını iptal eder** (Q40 (a)) → gerçek çalışanın sonraki dokunuşu §5 satır 3'e düşer, **kayıt yazmaz** ve fark edilir; kişinin kutusuna ve owner'lara *"a new phone was set up"* e-postası, panelde sinyal (§2.4.7) | işbirliği yapan iki kişi için parola, telefon devrinden **fazla bir şey** açmaz; risk 1 aynen kalır. **ADR 0005'e risk eki** (M16-50): *"parola (ya da parola linkini açan posta kutusu) + işverenin herhangi bir aktif plaketi = başka birinin kimliği; tespit kişinin ve owner'ın e-postası, panel"* |
| **Numaralandırma** | yol yok | link isteği: tek cevap + `[250,300] ms` bandı, eşleşmeyene e-posta yok; giriş: tek başarısızlık cevabı + K sabit iş + beş durumlu düzlük testi; işletme silme sayfası panel girişinin cevabı | başarı cevabı başarıyı söyler (doğru parolayla); 429 adresten bağımsız; e-postanın gelişi yalnız kutu sahibine bilgi verir |
| **Sıfırlama e-postasının kötüye kullanımı** (posta bombası, bütçe tüketme, taciz) | — | ADR 0049 §2.4'ün dört kovası; kişi başına tek canlı link; işverenin davetine dokunulmaz (§2.4.5) | işletmeler arası toplam K ile sınırlı; link istekleri ayrı tablodadır, ADR 0022 §9 bütçesinden pay yemez. Girişin bastığı `origin='password'` davetleri ise `employee_invites` satırıdır — §9 sayaçlarına girip girmediği **ölçülmedi** (sayaçlar yalnız e-posta kanalında koşar — ADR 0049 §2.3); girerse M16-52 sayımı `origin='employer'`'a daraltır |
| **Posta kutusu ele geçirme / içeriden** (paylaşılan kutu, yanlış adres, deaktive edilmemiş eski çalışan) | müdürün daveti aynı kutuya düşer | kutu = parola belirleme hakkı; ama giriş + **dokunuş** gerekir; her parola belirlemede kişiye, her parolalı aktivasyonda kişiye **ve** owner'lara e-posta (§2.3.5, §2.4.7); deaktive kişi hiçbir şey alamaz | ADR 0049 §4 bedel 3'ün aynısı: işverenin yazdığı adres bir kimlik doğrulayıcıdır. ADR 0049'un 7 günlük bekleme süresi **devralınmaz** — gerçek çalışan kendi parolasıyla hemen geri bağlanabilmeli; iki taraf da görünür |
| **bcrypt kapısını doldurmak** | panel girişi (ADR 0039 §5) | giriş ve parola sayfası da kapıdan geçer → kapı doluyken panel girişi, sıfırlama, kayıt onayı **ve** personel girişi 503 | tap, aktivasyon, panelin oturumlu yazmaları etkilenmez (ADR 0039 §5'in takası aynen, kapsamı genişler) |
| **Kayıt sihirbazının `client=app` bayrağını taklit** | — | bayrak yalnız çizilen bağlantıları azaltır (§2.6) | yok — bir saldırgan kendine daha sade bir sayfa çizer |
| **İşletme silme talebinin kötüye kullanımı** (eş-owner, çalınmış owner parolası) | — | yalnız aktif owner, parola kanıtı, işletme adını yazma; talep **hiçbir şeyi silmez ve işletmeyi durdurmaz**; öteki owner'lar ve operatör haber alır; operatör elle doğrular | çalınmış parolayla gelen talep isteyen owner'ın oturumlarını kapatır — gerçek owner sıfırlamayla döner |
| **Demo işletmenin inceleyici ya da sızan kimlik bilgisiyle kötüye kullanımı** | M16-39'un manager bedeli | (Q55 (b)) inceleyici yalnız çalışan kimliği alır: demo işletmeye `flag` satırları yazabilir (değişmez — §4.3), Saatlerim'i okur, kendi silme talebini gönderir; statik QR bağı **yalnız operatörün işaretlediği** işletmede | demo işletmede kalıcı çöp satırlar (kabul — demo verisi); her inceleme döngüsünden sonra parola değişir, cihaz panelden çıkarılır (M16-57) |
| **Tenant'lar arası okuma** | çözümleyiciler sırdan çözer | çözümleyici adresle çözer — `resolve_admin_by_email` (`00011`) emsali; dönen yalnız kimlikler (+ girişte özet); `tappa_resolver`'a **yalnız yeni sütunlar** verilir (`tenants.id`'ye dokunulmaz, §1.6) | sütun düzeyi GRANT'ları `privCatalogAllowed` pinler (M16-51) |

## 4. Bedeller

1. **Yeni bir kimlik bilgisi türü.** Parola tahmin edilebilir, paylaşılabilir, başka sitelerde yeniden kullanılabilir; sızmış
   parola kontrolü (dış servis) **yok** — gizlilik ve bağımlılık (CLAUDE.md §1). Telafi: parola bağlamaz (§2.1.3).
2. **Adres bir kimlik doğrulayıcıdır** (ADR 0049 §4 bedel 3'ün aynısı) — yanlış yazılmış adres o kutunun sahibine parola
   belirleme imkânı verir; bağ dokunuş ister, kişi ve işveren her olayı görür.
3. **Holding çalışanı işletme başına parola belirler** (§2.2); aynı parolayı iki satırda kullanırsa seçici çıkar.
4. **Çıkış kutusu dayanıklı değildir** — süreç ölümünde link gitmez, kişi yeniden ister (ADR 0049 §4 bedel 4).
5. **WorkGate'in takası genişler** (§2.5, §3).
6. **Mekân NAT'ı:** aynı Wi-Fi'daki personel bir kaynak adresi paylaşır; link isteğinde 10 dakikada altıncı kişi 429 alır
   (ekran *"use mobile data"* der); girişte 10 başarısızlık / 10 dk aynı adreste herkesi bekletir.
7. **Bellekteki kovalar pod sayısıyla çarpılır.**
8. **Uygulama modu bir sunum sözleşmesidir** — sihirbaza ya da yasal sayfaya yeni bir bağlantı ekleyen her değişiklik kapalı
   `href` listesinin testine takılır (bilinçli sürtünme).
9. **İşletme silme bir taleptir, anında silme değildir** — Apple'ın bunu kabul ettiği **ölçülmedi** (Apple'ın *"Offering account
   deletion in your app"* destek sayfası M16-50'de okunur); gerekçe CLAUDE.md §4.3 ve fatura kayıtlarının hukuki saklama süresi.
10. **Q55 (b) seçilirse** *"QR asla aktive etmez"* kuralına operatör bayrağına bağlı bir istisna girer (CLAUDE.md §5 cümlesi
    kodla aynı commit'te değişir); işaretli işletmede statik QR fotoğrafı + bir demo kimliği = yeni bir bağ.
11. **Uygulamadan kayıt e-posta onayıyla biter** (§1.7) — inceleyici çalışan bir posta kutusu kullanmazsa işletme doğmaz; bu,
    bugünkü web kaydının kendi kuralıdır ve gevşetilmez.

## 5. Kırmızı çizgiler

- **§4.1 Biyometri:** parola ya da giriş için biyometri API'si yok; sistem parola yöneticisi kendi kilidini kullanabilir —
  o telefonun katmanıdır, uygulama ondan veri almaz (ADR 0045 §2.3'ün anahtar kuralı aynen: `.biometryAny`, `.userPresence`,
  `setUserAuthenticationRequired` yasak).
- **§4.2 GPS:** giriş, parola, kayıt ve silme konum okumaz; konum yalnız tap anında (değişmez).
- **§4.3 `transactions` değişmez:** hiçbir yol satır silmez ya da güncellemez; işletme silme talebi bir taleptir; aktivasyon
  dokunuşu (statik QR dahil, Q55 (b)) satır yazmaz.
- **§4.4 Atomik `ctr`:** plaketle bağ `sun.Verify`'dan geçer, değişmez. Statik QR bağı (Q55 (b)) `ctr` taşımaz — koruma davetin
  tek ifadeli tüketimidir (canlı QR'ın şekli, ADR 0045 §2.3).
- **§4.5 Tenant izolasyonu:** yeni iki tablo §6 beşlisiyle; adres çözümü `tappa_resolver` sahipli dar definer'lar; sonrası
  `SET LOCAL app.tenant_id` ve açık `tenant_id` yüklemi; holding'de art arda ayrı `WithTenant`'lar. Operatör yalnız `op_*`.
- **§4.6 Kayıt kaybolmaz:** işletme silme talebi personeli durdurmaz; parolalı aktivasyonun gerçek çalışanı satır 3'e itmesi
  görünürdür (e-posta + panel) — ADR 0005 risk eki.
- **§4.7 Sırlar:** parolanın yalnız bcrypt özeti, linkin yalnız HMAC'i saklanır. **Log, hata ve audit `detail`'inde asla:**
  parola, adres, adresin özeti, link token'ı, davet kodu (CLAUDE.md §7; ADR 0022 §10). Demo işletmenin parolaları ve
  adresleri depoya asla — yalnız App Store Connect / Play Console (depo PUBLIC).

## 6. Mağaza ve gizlilik sonuçları

- **App Store gizlilik etiketi:** *Contact Info → Email Address* — toplanır, kişiye bağlı, *App Functionality*, izleme yok
  (giriş ve parola linki). Parola Apple'ın veri türlerinden biri değildir (**doğrula**, M16-58). Uygulama içi tarayıcıdaki kayıt
  formunun (işletme, mekân, owner adı/e-posta/parola, KDV no) etikete girip girmediği — **ölçülmedi**; M16-58 Apple'ın
  web görünümü kuralını okur, hukukçu ve kullanıcıyla yazar. `PrivacyInfo.xcprivacy` aynı satırla güncellenir.
- **Play Data safety:** *Personal info → Email address* — toplanır, paylaşılmaz, zorunlu (giriş), *App functionality, Account
  management*; *"Accounts: created by the employer outside the app"* (`google-play.md:115`) iki cümleye ayrılır: çalışan
  hesabını işveren açar; **işletme hesabı uygulamadan açılabilir** (uygulama içi tarayıcıda web sihirbazı). Silme: iki web
  URL'si — çalışan için `https://taptime.mt/legal/privacy` (bölüm 12 *"Without the app"*), işletme için
  `https://taptime.mt/business/delete` (§2.9; uygulamasız çalışır).
- **Gizlilik taslağı bölüm 12** (`privacy-draft.md`): *"the app uses no password and no sign-in cookie"* → *"you sign in with
  your work e-mail address and a password you choose; Taptime stores only a hash of the password, the app keeps neither on the
  phone, and signing in never sets up a phone on its own — your first touch of a plaque does"*; *"Activating the app"* paragrafı
  girişle başlar, davet kodu ikinci yol olur; e-postayla giriş köşeli parantezi (`:160`) bu metinle **değişir**; yeni
  paragraflar: işletme hesabını uygulamadan açmak (web sihirbazı, onay e-postası, panel bilgisayarda) ve **işletme hesabını
  silme talebi** (ne kapanır, ne kalır, operatöre nasıl gider). Hukukçu onayı kullanıcıdadır (M16-41'in kuralı).
- **İnceleme notu:** inceleyici demo çalışanın e-postası + parolasıyla girer → onay → (Q55'in yolu) bağ → statik QR'la FLAGGED
  kayıt → Saatlerim → Ayarlar → *Delete my account*; ayrıca *"Create a business account"* yolu ve *"Delete a business
  account"* sayfası; 3.1.3(c) (işletmelere doğrudan satılan, fatura dışarıda), 3.1.1 (uygulamada fiyat / satın alma yolu yok),
  3.2'ye karşı (her işletme web'den ya da uygulamadan kayıt olabilir — tek bir kurumun iç uygulaması değil), 4.8.

## 7. Karar verilmedi

- **Q55** inceleyicinin bağ yolu · **Q56** hangi işletmeler ve varsayılan · **Q57** işletme silme talebinin girişi ve etkisi ·
  **Q58** Android uygulama içi tarayıcısı · **Q59** davetli çalışan parola belirleyebilir mi.
- *Sonra:* tarayıcıda parolayla giriş (bu ADR yalnız uygulama); uygulamada *"Change password"*; panelden *"Send app sign-in
  e-mail"*; iOS `webcredentials:`; global çalışan kimliği (reddedildi, §2.2); işletme silme talebinin geri alma ucu.

## 8. Uygulama

- **M16-50:** kabul — Q55 … Q59 (AskUserQuestion), güncel Apple / Google metinlerinin okunması, canlı taban ölçümü (bir adresi
  taşıyan aktif işletme sayısının en büyüğü, K için), **ADR 0049'a tarihli not** (*"2026-MM-DD: §2'nin e-postayla tek kullanımlık
  link mekanizması ADR 0051'le yerini aldı; §2.4'ün bütçeleri parola linki isteğine, §2.6'nın görünürlüğü `origin='password'`
  aktivasyonuna, §2.7'nin iki kapısı ADR 0051 §2.8'e devredildi; metin silinmez"*), ADR 0026 / ADR 0045 / ADR 0022 / ADR 0039
  notları, **ADR 0005 risk eki**.
- **M16-51:** şema (00118, 00119) + parola linki isteği + `/staff/password` sayfası + env kapısı. **M16-52:** `POST /app/v1/signin`
  + `origin='password'` daveti + görünürlük. **M16-53:** iki uygulamanın ekranları. **M16-54:** sihirbazın ve yasal sayfaların
  uygulama modu. **M16-55:** işletme silme talebi (00120). **M16-56:** yalnız Q55 (b) — inceleme işletmesinde statik QR bağı
  (00121). **M16-57:** demo işletme ve inceleyici runbook'u (M16-39'u revize eder). **M16-58:** mağaza beyanları ve gizlilik
  taslağı (M16-27 / M16-41'i revize eder). **M16-59:** Faz H kapanışı.
- CLAUDE.md §5 *"Aktivasyon dokunuşu"* cümlesi parolalı girişi (ve Q55 (b) seçilirse işaretli işletmenin statik QR'ını) **kodla aynı
  commit'te** anar (M16-52, M16-56).
