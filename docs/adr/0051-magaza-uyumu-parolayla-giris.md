# ADR 0051 — Mağaza uyumu: çalışanın e-posta + parolayla girişi, uygulamadan işletme kaydı, işletme hesabını silme

- **Durum:** Önerildi (2026-11-25; aynı gün iki bağımsız plan denetiminin NO-GO bulgularıyla yeniden yazıldı — denetim
  kimlikleri metinde köşeli parantezde, ör. *[B-Y1]*, *[A-9]*) — kabul M16-50'de, kullanıcının. Açık sorular **Q55 … Q59**
  ([open-questions.md](../plan/open-questions.md)); cevapsız kabul yok, **önerilmeyen bir cevap planı yeniden yazdırır ve
  yeniden denetletir** (§7). Kabul edilince **ADR 0049 §2'nin link mekanizmasının yerini alır** (notu M16-50 düşer — §8).
- **Tarih:** 2026-11-25 · **Dal:** `m16-native-ui` (PR #49, `main` dışı; yalnız belge) · **Plan:**
  [docs/plan/m16-mobil.md](../plan/m16-mobil.md) → *Faz H — Mağaza uyumu* (M16-50 … M16-61)
- **Karar veren:** kullanıcı (ürün sahibi), 2026-11-25, AskUserQuestion — §2.1'in on maddesi (**1–7** ve **K-A, K-B, K-C**)
  **sabittir**. Geri kalan her şey **öneridir**.
- **İlgili:** ADR 0002 md. 7 · ADR 0005 · ADR 0010 · ADR 0021 · ADR 0022 (§6, §8, §9, §10) · ADR 0025 · ADR 0026 · ADR 0031
  (§5.6, §5.9, §6) · ADR 0039 · ADR 0045 (§2.3, §2.6, Q40 (a)) · ADR 0048 · **ADR 0049** (yerini alır) · CLAUDE.md §4, §5, §6,
  §7, §9 · open-questions Q13, Q23, Q28, Q36, Q40, Q44–Q49.
- **Atıf kuralı:** `file:line` atıfları **`m16-native-ui` `3eeb91f0`**'e göredir (`828ce9b1`'den yalnız belge farkı).
  **Okundu** = dosyadan okundu, çalıştırılmadı. **Ölçülmedi** = bu belgeyi yazan doğrulayamadı; ilgili kart ölçer. Apple ve
  Google metinleri ana oturumun ve denetçinin 2026-11-25 okumasıdır; bu belgeyi yazan sayfaları **yeniden açmadı** — M16-50
  okur ve uymayan noktada **durur**.

---

## 1. Bağlam — bugün (okundu)

1. **ADR 0049 hiç uygulanmadı.** `git grep -n "signin/request" -- '*.go'` → **0**. `/app/v1` yolları
   `internal/handler/app.go:207-224`; giriş yolu yok.
2. **Uygulamanın ilk ekranı davet kodudur** (iOS `mobile/ios/Taptime/ActivationCopy.swift:20`; Android
   `mobile/android/app/src/main/res/values/strings.xml:21` `code_body`). Sonrası ADR 0026'nın üç adımı: lookup → bind → complete
   (`internal/handler/appactivate.go:15-28`). **Lookup plaketsiz çalışır ve şunları döndürür:** `employeeName`, `businessName`,
   `locationName`, `wifiSsid`, marka (`appactivate.go:48-59`) — bir davet kodu (ve Faz H'de bir parola) bunları açar *[B-O5]*.
3. **Statik QR aktivasyonu tamamlamaz:** uygulama *"NFC only"* (`appactivate.go:249`), `!p.HasSUN()` → `appErrActivationNeedsTouch`
   (`:324-327`); tarayıcı aynı (`internal/handler/activate.go:978`). NFC'siz yollar M16-23'tür (`main`'de, PR #46): canlı QR ve
   müdür onayı (`internal/invite/approval.go:317`; 00115). **Müdür onayı telefonun müdürün karşısında olduğunu varsayar**
   (*"comparing the code on the phone in front of them"*, `approval.go:18-32`) *[B-O5]*.
4. **"Aktive" ⇒ "bir davet tüketildi":** `employees.status = 'active'`'i yalnız `ConsumeInviteAndActivate` yazar
   (`db/queries/invites.sql:72-97`). Davet yaratmanın tek yolu `IssueAndDeliver` (`internal/invite/manager.go:219`, `:255`); **basım
   kişinin öteki bekleyen davetlerini aynı transaction'da emekliye ayırır** — kullanıcı kararı 2026-08-08, gerekçesi ölçülmüş bir
   ele geçirme (`manager.go:237-254`, `:300-320`; sorgu `CancelPendingInvitesForEmployee`, `invites.sql:330-342`). **Tüketim
   (`consume`, `manager.go:873`) kardeş davetleri iptal etmez** — bugün basım kuralı kişi başına tek canlı davet bıraktığı için
   gerekmiyordu *[B-Y1]*.
5. **Davet bütçeleri her satırı sayar:** `CountRecentInvites` — *"EVERY ROW COUNTS, whatever became of it"* (`invites.sql:410-425`);
   `checkLimits` kiracı kilidini (`LockTenantForInviteLimits`, `invites.sql:~403-408`) alır ve sayar (`internal/invite/email.go:398-422`)
   — **yalnız e-posta kanalında** (`email.go:394-403`) *[B-O2]*.
6. **Parola işi bugün iki pakette:** `golang.org/x/crypto/bcrypt` yalnız `internal/adminauth/password.go` ve `internal/operatorauth`
   (`operatorauth.go`, `password.go`) içe aktarır (ölçüldü: `git grep -ln '"golang.org/x/crypto/bcrypt"' -- '*.go' ':!*_test.go'`).
   cost 12 (`password.go:33-49`), `Hash` `:180`, `Compare` `:324`, `CompareDummy` `:346`, 72 bayt (`:132`); en kısa 8 karakter
   (`internal/domain/signup/signup.go:130`). **`WorkGate`'in dışa açık yöntemi yoktur** — yalnız `WithWorkGate` seçeneği
   (`internal/adminauth/workgate.go:179-273` küçük harfli, `:286-292` seçenek); tek kapıyı `TestWorkGateWiring_OneGateServesEveryPasswordService`
   pinler (`cmd/tappa/workgatewiring_test.go:21`) *[A-12]*. Prod: 1 slot, bekleme 10 sn, kuyruk 2 (ADR 0039 §2). Panel girişi
   `1 + MaxCandidates` = 9 karşılaştırma (`internal/adminauth/manager.go:163`). Başarısız giriş bütçesi 10 / 10 dk; kodun kendi hesabı
   *"10 failures x 8 candidates x ~380 ms = ~30 s"*, kimlik yolunda 9 ile ~34 sn (`internal/handler/adminratelimit.go:655-681`).
   Hesap izi 10 / 10 dk **kapatmaz** (`:762-803`). **Bütçeyi karşılaştırmadan önce tek kilitli adımda ayırma dersi:** Allowed-then-Charge
   ile 40 eşzamanlı tahmin 40 karşılaştırma yaptı, sınır 10'du; çözüm `TryCharge` (`internal/handler/signupconfirm.go:586-594`) *[B-D5]*.
7. **Kimlik kanıtı oturumsuz hazır değildir:** `AuthenticatePerson` oturum açar (`internal/adminauth/identity.go:129-136`);
   `proveIdentityPassword` bir `SignupClaim`'e bağlıdır (`signupconfirm.go:583-597`); `RevokeAll` bir çerez oturumu ister
   (`internal/adminauth/identityops.go:196-206`) *[A-15]*.
8. **`employees.email` tekilliği `00037`'nin kısmi indeksidir:** `(tenant_id, email) WHERE email IS NOT NULL AND status <> 'deactivated'`
   (`db/migrations/00037_*.sql:96-105`; `00003`'ün ilk indeksinin yerini aldı) — aynı kişi iki işletmede iki satır, aynı işletmede
   deaktive bir satır + yeni bir satır *[B-D4]*. Adresle çözüm emsali `00011:288-330` (`OPERATOR(public.=)`). **Sıralama dersi:**
   `ORDER BY tenant_id` rastgeledir; saldırgan kurbanın adresini taşıyan yeni işletmeler açıp gerçek satırı pencere dışına iter
   (ölçülmüş: 5 koşunun 3'ünde) — çözüm **en eski önce** (`db/migrations/00017_*.sql:30-60`, `:91`, `:195`) *[B-Y2]*. `tappa_resolver`'ın
   bugünkü `employee_invites` yetkileri: `SELECT (id, tenant_id, employee_id, code_hash, expires_at, used_at)` (`00009:242-243`) +
   `(cancelled_at)` (`00012:87`) — `resolve_invite_by_code_hash` bunlara bağlıdır *[A-11]*; `tenants.id` `00051:155`'ten beri.
9. **`tenants`:** `vat_number text NOT NULL UNIQUE` (`db/migrations/00001_create_tenants.sql:15`; gevşeten migration bulunmadı —
   `grep`); `tappa_app`'ın `INSERT`'i sütun düzeyindedir (`00016:85-89`) *[B-D3]*. **Operatör yüzeyinde işletme yaratan yol yoktur**
   (ölçüldü: `OperatorDB`'nin yöntemleri `internal/db/operatorpool.go:496-706` — listele, ayrıntı, API, plaketler, audit, fatura, KDV,
   holding, askı, OAuth; yaratma yok; migration'larda `op_create_*` tenant fonksiyonu yok) *[K-C]*. Askı `op_suspend_tenant(p_session,
   p_tenant_id, p_reason)` (`00063:943`); okuma fonksiyonlarının güncel tanımı `op_read_tenants` `00063:800`, `op_read_tenant_detail`
   `00063:864` *[A-16]*.
10. **Kayıt sihirbazı:** üç adım (`signup.go:31-33`); **KDV numarası zorunlu ve biçimi denetlenir** (`internal/domain/signup/signup.go:319-351`);
    son POST yalnız taslak açar ve onay linki e-postalar, işletme onayda doğar (`signupconfirm.go:23-44`); `SignupDone` giriş sayfasına
    düğme verir (`:562-571`; `signup.templ:697-698`) — **panel kendiliğinden açılmaz**. Ödeme adımı yok; faturalama paneldeki aylık
    faturadır (`internal/handler/billing.go`).
11. **Kabuk bağlantıları (ölçüldü):** `web/templates/pages/publicchrome.templ` — `:170` telefonun *Start free* çubuğu, `:180` marka →
    `/`, `:186` `LandingNav` (*Product · Security · Pricing*, `landingview.go:466-470`), `:197-205` *Sign in* / *Start free*, `:213`
    telefon menüsü `PublicProductLinks` (*Pricing* dahil, `landingview.go:502-509`), `:277` *"Built by"* (`layout.CreditURL`), `:281`
    altbilgi ürün bağlantıları, `:292` `LegalPages`. Yasal sayfa aynı kabuk (`legal.templ:63`); gövdede iç ve dış bağlantılar
    (`legal.templ:158-163`, `legalExternal`), taslakta mutlak `https://taptime.mt/legal/cookies` (`docs/legal/privacy-draft.md:68`) ve
    `mailto:`. Uygulama bugün bu sayfalara link verir (`ActivationViews.swift:302`, `MainActivity.kt:319`).
12. **Universal Link / App Link** yalnız `TapPath` ve `/q` (`internal/handler/applinks.go:70`).
13. **Ingress:** erişim log'u kapalı tek Ingress `tappa-panel`'dir, yolları `/admin`, `/oauth` (`deploy/k8s/40-ingress.yaml:226-259`,
    `enable-access-log: "false"`); pin `TestPackaging_ThePanelPathsAreRoutedWithoutAnAccessLog` (`cmd/tappa/panelingress_test.go:45`).
    Başka her yolun sorgu dizgesi ingress erişim log'una düşer *[B-O1]*.
14. **E-posta kesicisi:** `SendExempt` sayılır ve asla reddedilmez (`internal/mail/breaker.go:29-44`); muaf gönderimler pencereyi
    doldurup **reddedilebilir** gönderimleri kapatabilir (`:76-78`); pencere süreç başınadır (`:112-119`). Owner bildirimleri: kuyruk 32,
    2 işçi, owner + tür başına günde 20 (`internal/handler/ownernotice.go:60-74`; başlık `:20-38`) *[B-O3]*.
15. **Çalışanın silme talebi vardır** (M16-37, **bu dalda**: `786bd004`, `13f24033`, `cca8bad8`): talep + her yerde çıkış + audit +
    owner e-postası (`internal/domain/tenant/staffdeletion.go:3-25`).
16. **Log taraması:** `password` zaten `R7_TRIGGERS`'tadır (`scripts/redline-check.sh:2382`), `e_?mail` R7M'dedir (`:2642`); R7A
    (`:2589-2593`) yalnız `app*.go`'yu tarar ve `e_?mail` taşımaz *[A-14]*.
17. **Mağaza ve gizlilik belgeleri** bugünü anlatır (`docs/store/app-store.md:63`, `:68-75`; `docs/store/google-play.md:110`, `:115`;
    `privacy-draft.md:148`, `:150`, `:160`).
18. **Neden şimdi** (ana oturum ve denetçi, 2026-11-25): App Review **2.1(a)** inceleme hesabı ya da Apple onaylı demo modu + gereken
    kaynak (*"a sample QR code"*); **5.1.1(v)** uygulama hesap yaratabiliyorsa uygulama içi silme; Apple *"Offering account deletion in
    your app"*: *"only offering to temporarily deactivate … is insufficient"*, *"should not require people to … send an email … or other
    support flows"*, *"Inform the person how long it will take … and provide a confirmation when the deletion is complete"*; Google:
    hesap silme *"must also delete the user data associated with that app account"*; Play'de **tek** bir silme URL'si alanı *[B-O11]*;
    **4.8**, **3.1.3(c)**, **3.1.1**, **3.2** (forum retleri, topluluk kaynağı — 740245, 755917, 704518, 126794; 811018; 724032).

## 2. Karar

### 2.1 Kullanıcı kararları (2026-11-25) — sabit

1. **"Sign up" = işletme kaydı;** ilk ekran **"Sign in"** + **"Create a business account"** (web `/signup`, uygulama içi tarayıcıda).
2. **Çalışan girişi = e-posta + parola;** ADR 0049'un tek kullanımlık linkinin yerini alır.
3. **Cihaz bağı ilk plaket / QR dokunuşunda;** parola tek başına hiçbir cihazı bağlamaz.
4. **Uygulama içi kayıt "uygulama modu"nda biter;** uygulamadan fiyata, panele, Billing'e yol yok.
5. **İşletme hesabını silme:** owner'ın uygulama içi talebi; oturumlar kapanır, operatöre ulaşır, kayıtlar kalır.
6. **Demo işletme "Taptime Demo";** inceleyici demo bir çalışanın e-postası + parolasını yalnız konsollarda alır.
7. Önce Play iç testi; üretim PR #49'u, hukukçu onaylı metni, `TAPPA_APP_ANDROID_SIGNER_SHA256`'yı bekler; ekran görüntüleri demodan.
8. **K-A — silme 30 gün içinde tamamlanır.** Sayfa *"within 30 days"* der. Operatör işletmeyi askıya alır (ADR 0025) ve owner'ların
   ve çalışanların **kişisel verisini anonimleştirir** (ad, e-posta, parola özeti, kimlik bilgileri). **Giriş-çıkış ve fatura kayıtları
   yasal saklama için kalır** ve sayfa bunu açıkça söyler. Tamamlanınca owner'a **onay e-postası**. **Hukukçu onayı** ister ve **Q13
   (saklama) ön koşuldur** — ikisi de kapıdır.
9. **K-B — "Built by" altbilgi bağlantısı** (`layout.CreditURL`, `publicchrome.templ:277`) uygulama modunda **kalır**; satın alma yolu
   değildir (2026-10-23 kararı).
10. **K-C — "Taptime Demo" operatör yüzeyinden, KDV numarasız** açılır; `/signup`'a sahte KDV girmez. Operatör yolu yoksa (§1.9: yok)
    bir kart yaratır (`op_*` SECURITY DEFINER, ADR 0021, `operator_audit_log`) — **M16-60**.

### 2.2 Kimlik bilgisi: çalışan satırı başına bir parola özeti

- **Tablo `employee_passwords`** (00118): `tenant_id`, `employee_id`, `digest`, `set_at`; `UNIQUE (tenant_id, employee_id)`; §6 beşlisi;
  `tappa_app`'a `SELECT, INSERT, UPDATE (digest, set_at)` — `DELETE` yalnız M16-61'in anonimleştirme definer'ında.
- **Kod `internal/adminauth`'ın içindedir** (yeni dosya, öneri `staffpassword.go`, tür `StaffPasswords`): `WorkGate` dışa açılmaz, yeni
  servis `WithWorkGate` ile kurulur ve `TestWorkGateWiring_OneGateServesEveryPasswordService` genişler *[A-12]*. bcrypt yalnız var olan
  `Hash` / `Compare` / `CompareDummy`.
- **Parola kuralı:** panelinki (8 karakter, 72 bayt) **ve** gömülü, bağımlılıksız bir **yaygın parola ret listesi** (`go:embed` ile bir
  metin dosyası; kaynağı ve lisansı M16-51 ölçer ve dosyanın başına yazar; liste bir sır değildir) — ret metni *"That password is too
  common. Choose another."* *[B-O5]*.
- **Satır başına, global kimlik değil** (gerekçe: ADR 0031'in global kimliği §4.5'in bilinçli istisnası; çalışanın bağı zaten tenant
  başına). Bedel: holding çalışanı işletme başına parola belirler.
- **Telefonda parola yok;** alanlar sistem parola yöneticisi için işaretlenir. Girişin döndürdüğü **aktivasyon kodu** tamamlanana ya da
  süresi dolana dek uygulamanın güvenli deposunda (iOS Keychain `WhenUnlockedThisDeviceOnly`, Android Keystore'la şifreli tercih)
  tutulur — bugünkü yapıştırılmış davet kodunun eşi.

### 2.3 İlk parola ve sıfırlama: tek akış, e-postadaki link

1. **İstek** `POST /app/v1/password/request` — her durumda 202, `[250, 300] ms` bandı, eşleşmeyene e-posta yok, kaynak kovası 429.
2. **Kim alır:** env + owner ayarı açık (§2.8), askıda olmayan işletmede (a) **aktif** çalışan ya da (b) **işverenin canlı davetini
   taşıyan davetli** (Q59 önerisi). **Yönetici adresi dışlaması SQL'de değil, Go'da, her işletmenin kendi `WithTenant`'ında** (var olan
   `ErrAddressIsAdministrators` okuması — `tappa_resolver`'a `admin_users` yetkisi verilmez) *[A-10]*.
3. **Çözümleyici** `resolve_staff_password_targets(p_email citext, p_tenants uuid[], p_all boolean)` — env listesi `uuid[]` olarak,
   `*` ise `p_all = true` ve dizi boş *[A-9]*; `SECURITY DEFINER`, sahibi `tappa_resolver`, `SET search_path = pg_catalog, pg_temp`,
   `OPERATOR(public.=)`; **bütün süzgeçler `LIMIT`'ten önce** (durum, `email IS NOT NULL`, `t.suspended_at IS NULL`,
   `t.staff_password_signin`, env), sıra **`(e.status <> 'active'), t.created_at, e.created_at, e.id`** — aktif önce, sonra en eski
   işletme, en eski satır; `LIMIT 3` *[B-Y2]*. Yeni yetkiler yalnız yeni sütunlar: `employees (id, tenant_id, email, status, created_at)`,
   `tenants (suspended_at, staff_password_signin, created_at)`; Q59 (b) ise davetin canlılığı için **`employee_invites (origin)`** (tek
   yeni sütun — öteki sütunlar `00009`/`00012`'den beri verili, **yeniden verilmez, `Down`'da geri alınmaz**) *[A-11]*.
4. **Link** `employee_password_links` (00118): 256 bit token, **HMAC'i yeni env `TAPPA_STAFF_PASSWORD_HMAC_KEY`** ile (32 bayt; env listesi
   boş değilse zorunlu — açılışta yoksa ret; değeri prod sırlarındadır, depoda asla) *[A-13]*; ömür 1 sa; kişi başına tek canlı link;
   çıkış kutusu (`adminresetoutbox.go` biçimi). Bütçeler yeni kiracı kilidinin altında: `LockTenantForStaffPasswordLimits`
   (işletme başına bir danışma kilidi — `LockTenantForInviteLimits`'in `invites.sql:~408`'deki biçiminin eşi, kendi önekiyle) *[A-13]*.
5. **Sayfa** `GET/POST /staff/password` — GET tüketmez (`readLink`), POST `Hash`'i kapıda koşar; tek transaction'da özet, link `used_at`,
   kişinin öteki canlı linkleri `revoked_at` **ve kişinin bekleyen `origin='password'` davetleri `cancelled_at`** (bir saldırganın basıp
   henüz bağlamadığı kod ölür) *[B-O7]*. Kişinin kutusuna *"Your Taptime password was set"*. `/staff` **erişim log'suz Ingress'e girer**
   (§2.11) *[B-O1]*.
6. **Gönderici:** kişiye giden bildirimler (parola belirlendi, yeni telefon kuruldu, işletme silme talebi alındı / tamamlandı) yeni bir
   **`personNotices`** kuyruğundan gider — `ownerNotices` yalnız owner'lara yazar (`ownernotice.go:20-38`); biçimi aynı (kuyruk 32, 2 işçi,
   `SendExempt`), sınırı kişi + tür başına günde 5 *[A-19]*.

### 2.4 Giriş: parola → aktivasyon daveti → ilk dokunuşta cihaz

1. **Sıra:** kaynak taşma tavanı → kaynak deneme bütçesi (**`TryCharge`, karşılaştırmadan önce**) → `WorkGate` → aday çözümleyicisi →
   **tam K = 3 karşılaştırma** (`CompareDummy` dolgusu) → karar.
2. **Aday çözümleyicisi** `resolve_staff_signin_candidates(p_email citext, p_tenants uuid[], p_all boolean)` — §2.3.3'ün süzgeçleri ve
   sırası **artı `JOIN employee_passwords` `LIMIT`'ten önce** (parolası olmayan satır pencereye girmez: saldırganın açtığı işletmelerdeki
   kurban-adresli satırların parolası yoktur, çünkü link kurbanın kutusuna gider) *[B-Y2]*; döner `(tenant_id, employee_id, digest)`.
3. **Tek başarısızlık cevabı** `401 {"error":"signin_failed"}` — beş durumun düzlüğü ADR 0031 §5.6 biçiminde ölçülür.
4. **Başarı bir oturum ya da cihaz değildir.** Doğrulanan her satır için o işletmenin `WithTenant`'ında (art arda) `IssueAndDeliver` →
   `origin = 'password'`, **ömür 24 saat** (Q44 (a)'nın sayısı; `minTTL`–`maxTTL` içinde — evde giriş, ertesi sabah kapıda dokunuş),
   kodu çağırana veren kanal (`ManagerVisibleChannel` biçimi). **`IssueAndDeliver`'ın davranışı değişmez:** basım kişinin bütün bekleyen
   davetlerini — **iki kökenden de** — iptal eder (2026-08-08 kuralı). İki yönde: işverenin basımı bekleyen parola davetini, parolalı giriş
   işverenin bekleyen davetini emekliye ayırır; parolayı bilen kişi işverenin davetinin yerine geçer (Q59 (b)'de davetli kişinin bedeli:
   24 saatte tamamlamazsa müdürden yeni davet) *[B-Y1, A-18]*. Kanal e-posta olmadığı için `checkLimits` koşmaz; **parola yolu
   `LockTenantForInviteLimits`'i `IssueParams.Gate` kancasında açıkça alır** ve kendi sınırını sayar (§2.5) *[B-O2]*.
5. **Tüketim kardeşleri iptal eder** *[B-Y1]*: **her** başarılı tüketim — tarayıcı (`Manager.Activate`), uygulama (`ActivateDevice`), canlı QR,
   müdür onayı (`ActivateApproved` / `ActivateDeviceApproved`) ve Q55 (b)'nin statik QR'ı — `ConsumeInviteAndActivate`'ten hemen sonra **aynı
   transaction'da** `CancelPendingInvitesForEmployee`'yi (köken ayırmadan) çağırır; tek yer `consume`'un `after` kancasından önceki
   ortak adımdır (`manager.go:873`), statik bir test her tüketen çağıranın bu yoldan geçtiğini sınar. Sonuç: parolalı aktivasyondan sonra
   işverenin eski kodu ikinci bir telefon bağlayıp gerçek çalışanı düşüremez.
6. **Görünürlük** (ADR 0049 §2.6, `self` → `password`): owner'lara bildirim (aşağıda özet kuralı), kişiye bildirim (`personNotices`),
   panelde *"Set up by app sign-in on <date>"*, anomali ekranı; **müdür onayı listesinde parolalı girişten gelen istek *"via password sign-in
   — verify the person in front of you"* uyarısı taşır** *[B-O5]*; T59 (a) `tap.signed_out_phone`.
7. **Owner bildiriminin özeti** *[B-O3]*: `password_activation` türünde owner başına **saatte en çok bir** e-posta — ilk olay hemen,
   sonrakiler o saat içinde sayılır ve saat dolunca tek bir *"N phones were set up by password sign-in in the last hour"* gider; panel
   sinyali dayanıklıdır, e-posta değildir.
8. **Kurtarma yolu** *[B-O7]*: parolayı değiştirmek bağlı cihazları **iptal etmez**. Gerçek çalışan yeni parola belirler (bekleyen parola
   davetleri ölür, §2.3.5), girer ve bir plakete dokunur → Q40 (a) saldırganın cihazını iptal eder; ya da müdür *"Sign out phones"* der
   (T59). *"A new phone was set up"* e-postası bunu söyler: *"If this wasn't you: set a new password in the app, sign in and tap a plaque at
   work — that signs the other phone out — and tell your manager."*
9. **Kilit yok** (panelin kararı); telafi §2.5'in bütçeleri, ret listesi, SigNoz uyarısı (§2.11) ve fiziksel bağ.

### 2.5 Bütçeler

Her bütçe **karşılaştırmadan önce tek kilitli adımda ayrılır** (`TryCharge`; `signupconfirm.go:586-594`'ün ölçülmüş dersi), kapı ret
verirse iade edilir *[B-D5]*.

| Kova | Değer | Nerede | Gerekçe |
|---|---|---|---|
| giriş — kaynak, başarısız | 10 / 10 dk | bellek | `adminAttemptLimit` |
| giriş — kaynak, karşılaştırmaya ulaşan | 30 / 10 dk | bellek | 30 × 3 × 0,38 sn ≈ 34 sn CPU — panelin kimlik yolu başarısız bütçesiyle aynı pay (`adminratelimit.go:655-681`); hesap, ölçülmedi |
| giriş — hesap izi | 10 / 10 dk + tek `FirstOverLimit` | bellek | `adminAccountLimit`: kapatmaz |
| süreç | `WorkGate` | bellek | ADR 0039 |
| parola daveti — kişi | **5 / sa** | DB, `LockTenantForInviteLimits` altında | `origin='password'` satırlarından |
| link isteği — kaynak / süreç / işletme / kişi | 5 / 10 dk + 20 / gün · 100 / sa · 10 / sa + 30 / gün · 1 / sa + 3 / gün | bellek · bellek · DB · DB | ADR 0049 §2.4 |
| parola sayfası — link başına geçersiz form | 10 / 10 dk | bellek | `signupClaimLinkLimit` biçimi (`signupconfirm.go:74-83`) |

- **İşverenin e-posta davet bütçesi daralır:** `CountRecentInvites` `origin = 'employer'` satırlarını sayar (`invites.sql:410-425`'in
  *"every row counts"* cümlesi *"every employer row counts"* olur) — parolalı girişler müdürün 50/sa'ini yemez *[B-O2]*.
- **Küresel tavan, hesap** *[B-O5]*: dağıtık kaynaklı tahmin yalnız `WorkGate`'le sınırlıdır — prod'da 1 slot, kuyruk 2; bir giriş ≈ 3 × 0,38 =
  1,14 sn → **≈ 0,88 tahmin/sn ≈ 76 000 / gün** ve o sırada **panel girişleri 503** alır. IPv6 kovaları /64 başınadır (ADR 0039 §3). Bunlar
  hesaptır, **ölçülmedi**; telafisi ret listesi, uyarı ve fiziksel bağdır.

### 2.6 Uygulama içi kayıt: "uygulama modu"

- Uygulama `https://taptime.mt/signup?client=app`'i iOS'ta `SFSafariViewController`, Android'de Q58'in yoluyla açar. Bayrak imzalı duruma
  (`signupstate.go`) ve taslağa yazılır, **yalnız sunumdur**; güvenlik kontrolleri bayt bayt aynı.
- **Uygulama modunda kaybolan bağlantılar** *[A-7]*: `publicchrome.templ:170` (*Start free* çubuğu), `:180` (marka `/`'a bağlanmaz,
  düz metin), `:186` (`LandingNav`), `:197-205` (*Already with Taptime?*, *Sign in*, *Start free*), `:213` (telefon menüsü), `:281`
  (altbilgi ürün bağlantıları). **Kalanlar:** `:277` *"Built by"* (**K-B**), `:292` yasal sayfalar (`?client=app` ile), yasal gövdedeki
  iç yasal bağlantılar (mutlak `https://taptime.mt/legal/*` dahil — `?client=app` ile yeniden yazılır), dış bağlantılar
  (`legalExternal`) ve `mailto:` (satın alma yolu değildir), sayfa içi çapalar.
- **Son ekranlar:** `/signup/sent` ve (taslak `client=app` taşıyorsa) `SignupDone` *"your business is ready — open the dashboard on a
  computer"* der, panel düğmesi yoktur. **İnceleyici çoğunlukla 1. adımda, KDV alanında durur** (`signup.go:319-351` — gerçek bir AB KDV
  numarası ister; sahte numara girilmez, K-C) — inceleme notu bunu söyler *[B-O10]*.
- `/signup*`, `/admin/signup/confirm`, `/staff/password`, `/account-deletion` `appLinkPaths`'e girmez.

### 2.7 İnceleyici plaketsiz nasıl bağlanır — Q55

Öneri **(b)**: operatörün işaretlediği inceleme işletmesinde (`tenants.store_review_at`, yalnız `op_set_store_review`) statik QR bağlar;
koruma davetin tek ifadeli tüketimi + §2.4.5'in kardeş iptali; başka her işletme bayt bayt bugünkü gibi. (a) Q47 (a) — ayrıca manager
girişi. (c) canlı QR — panel girişi ister. (d) nöbetle onay — reddedildi.

### 2.8 Hangi işletmelerde açık — Q56

Öneri **(b)**: env `TAPPA_STAFF_PASSWORD_TENANTS` (boş = üç yol bağlanmaz; UUID listesi; `*`) + owner ayarı
`tenants.staff_password_signin`: var olanlar kapalı, bu özellikten sonra kaydolanlar ve **operatörün açtığı işletmeler** açık doğar.
**`signup.Provision` sütunu `INSERT`'te değil, aynı transaction'da bir `UPDATE` ile yazar** — `tappa_app`'ın `INSERT` yetkisi sütun düzeyidir
(`00016:85-89`) ve değişmez; `GRANT UPDATE (staff_password_signin)` 00118'dedir *[B-D3]*.

- **Açılış işletme işletme** *[B-O3, B-O6]*: config PR 1 = yalnız demo; sonra her işletme için ayrı bir config PR'ı ya da `*`'a geçiş —
  **`*` (config PR 2) ancak §2.11'in taban ölçümü yapıldıktan sonra.**
- **KF/KM** *[B-O4]*: owner ayarı kapalıyken personel uygulamada *"Sign in"*'e basarsa 401 alır, env boşken 404 alır. Uygulama **404'ü ve
  401'i** *"Your workplace hasn't turned on app sign-in. Have an invitation code? Use it instead."* (öneri) diye karşılar ve *"Have an invitation
  code?"* yolunu öne çıkarır. **Mağaza üretimi KF/KM owner'larının kararına bağlıdır** (aç ya da personele davet kodu yolunu duyur) — kapı.
- **Bedel** *[B-D7]*: adresi aynı işletmede bir yönetici adresine eşit olan owner-çalışan parolayla giremez (`ErrAddressIsAdministrators`) —
  davet koduyla girer; ekran bunu söylemez (numaralandırmasızlık).

### 2.9 İşletme hesabını silme — K-A; giriş yeri Q57

- **Tek sayfa, iki yol** *[B-O11]*: `https://taptime.mt/account-deletion` (öneri yol; Play'in **tek** URL alanı). Bölüm 1 *"I work at a
  business that uses Taptime"* — uygulamada *Settings → Delete my account*; uygulamasız: işverene ya da Taptime'a yazmak (gizlilik bölüm 12).
  Bölüm 2 *"I own a business on Taptime"* — form. Uygulamada *"Delete a business account"* bu sayfayı `?client=app` ile açar.
- **Kim:** işletmenin **aktif owner**'ı; **askıdaki işletmenin owner'ı da** gönderebilir (askı bir ödeme ya da operatör kararıdır, silme hakkını
  kaldırmaz) *[B-D6]*.
- **Kanıt** (Q57 (a)): yeni yöntem **`adminauth.Manager.ProveOwnership(ctx, email, password, budget) (OwnershipProof, error)`** *[A-15]* —
  sözleşme: `AuthenticatePerson`'ın işi ve zamanlaması (kimlik yolu + eski kol, `1 + MaxCandidates` karşılaştırma, beş durum düz), **oturum
  açmaz**; dönen kanıt doğrulanan kimliğin (ya da eski, bağlanmamış `admin_users` satırının — eski kolun kuralıyla) **aktif `owner`** olduğu
  işletmelerin listesidir; manager üyeliği ve `disabled` satır listede yoktur. Panel girişinin bütçelerini ve `WorkGate`'i paylaşır.
  **Kanıttan sonra ek doğrulama yok** (e-posta onayı, destek akışı yok — Apple) *[B-Y4]*.
- **Sayfanın söyledikleri** (K-A; metin hukukçu onaylı): *"Your business account will be deleted within 30 days. We suspend the business and
  remove the personal data of its owners and staff: names, e-mail addresses, passwords and sign-ins. Clock-in and invoice records are kept for
  the period the law requires, without names. We e-mail you when it is done."* Son adım işletme adını yazdırır.
- **Talep — tek transaction** (00121): `tenants.deletion_requested_at`; `audit_log` `tenant.deletion_requested`; isteyen owner'ın bu işletmedeki
  panel oturumları — yeni **`adminauth.Manager.RevokeMembershipSessions(ctx, tenantID, adminUserID)`** (RevokeAll çerez ister; yeni yöntemin
  hangi definer'ı çağıracağı ölçülmedi, M16-55 ölçer) *[A-15]*. Commit'ten sonra: **isteyene** *"We received your request"* (`personNotices`,
  tek owner'lı işletmede de) *[B-O7]*, **öteki** owner'lara `ownerNotices` (`kind = business_deletion_request`; iş isteyenin `admin_users`
  kimliğini dışlar — yeni alan `exclude`) *[A-17]*. Operatöre e-posta yolu **yoktur** (§1.9; `opnotice_test.go` bir arayüz testidir, bildirim
  kanalı değil) → **varsayılan:** operatör yüzeyinde *"Deletion requests"* listesi (en eski önce, kalan gün) + adlandırılmış bedel: operatör
  listeye her iş günü bakar *[A-6]*.
- **Tek yönlü** *[B-O8]*: tetikleyici **`BEFORE UPDATE OF deletion_requested_at`** — `NULL` → değer serbest; dolu değeri değiştirmek ya da
  silmek yalnız yeni `op_cancel_business_deletion(p_session, p_tenant_id)` definer'ında (tetikleyici `current_user`'ı definer'ın sahibiyle
  karşılaştırır), `operator_audit_log`'la. Owner'ın geri alma ucu yoktur; owner operatöre yazar, operatör 30 gün içinde iptal edebilir.
- **Tamamlama — operatör, 30 gün içinde** (M16-61, 00123): `op_complete_business_deletion(p_session, p_tenant_id)` — askıya alır
  (`op_suspend_tenant`'ın etkisi), **anonimleştirir**: `employees`'in kişisel sütunları (ad, ad/soyad, e-posta, çalışan kodu, dış kimlik —
  kesin liste şemadan M16-61 çıkarır), `employee_passwords` ve `employee_password_links` satırları silinir, oturumlar ve cihazlar iptal,
  `admin_users`'ın adı/e-postası; ADR 0031 kimliği **yalnız başka aktif üyeliği yoksa** anonimleşir (aksi hâlde yalnız bu üyelik kapanır) —
  kimlik tablolarının definer'ları **ölçülmedi**, M16-61'in güvenlik merceği ister. **Kalanlar:** `transactions` (§4.3), `transaction_reviews`,
  `punch_claims`, `audit_log`, `billing_periods`, `tags` (plaket satırı asla silinmez), `tenants` satırı (adı korunur — fatura kaydı).
  `deletion_completed_at` yazılır; commit'ten **sonra** isteyen owner'a onay e-postası (adres tamamlamadan önce aynı definer'dan okunur, yalnız
  bellekte tutulur). **Ön koşullar:** Q13 cevaplı (saklama süresi) ve hukukçu onaylı metin — ikisi yoksa M16-61 açılmaz.
- **Runbook (operatör, sonuna kadar)** *[B-Y4]*: (1) liste her iş günü; (2) gün 0–25 içinde owner'a soru **sorulmaz** (Apple: ek akış yok);
  (3) gün ≤ 30 `op_complete_business_deletion`; (4) onay e-postasının gittiği `audit_log`/`operator_audit_log`'dan okunur; (5) iptal yalnız
  owner'ın yazılı isteğiyle, `op_cancel_business_deletion`. Gün sayımı operatör listesinde görünür.

### 2.10 Demo işletme operatörden — K-C

- **M16-60** (00122): `op_create_business(p_session, p_name, p_timezone, p_owner_email)` — `SECURITY DEFINER`, sahibi `tappa_opdefiner`,
  `operator_audit_log`; `tenants.vat_number`'ı yalnız bu yol için `NULL` bırakır: `ALTER COLUMN vat_number DROP NOT NULL` + `CHECK
  (vat_number IS NOT NULL OR created_by_operator)` (yeni sütun `created_by_operator boolean NOT NULL DEFAULT false`, `NOT VALID` + `VALIDATE`
  — 00109/00110 deseni); `UNIQUE` `NULL`'ları ayırt etmez, sorun yok. Owner'ı bir yönetici davetiyle bağlar (var olan
  `AdminInvites`; kabul `AcceptNew`). `vat_number`'ı okuyan her yer (fatura, operatör ayrıntısı, CSV) `NULL`'u çizer — listesi **ölçülmedi**.
- `/signup` değişmez; sahte KDV hiçbir yere girmez.

### 2.11 Sevk, taban ölçümü, uyarılar, geri alma

- **Hangi PR** *[B-O9]*: PR #49 (`m16-native-ui`, 37 commit) önce `main`'e girer (kullanıcı; birleştirme = deploy; `main`'de 00108–00117
  zaten var — ölçüldü). Faz H kodu #49'dan sonra `main`'den açılan ayrı bir dalda ve **ayrı bir PR'da** yürür (öneri dal `m16-store`).
  **Birleştirme öncesi kontrol listesi:** dalda tam CI yeşil (`make check`, `make audit`, iki mobil paket) · iki mercek ONAY · migration
  numaraları `main`'e karşı yeniden ölçüldü · env'ler boş girer (`TAPPA_STAFF_PASSWORD_TENANTS`), yeni sır (`TAPPA_STAFF_PASSWORD_HMAC_KEY`)
  prod sırlarında · Ingress değişikliği (`/staff`, `/account-deletion` erişim log'suz) ayrı bir `kubectl apply`'dır — **deploy onayı ister**
  *[B-O1]* · gizlilik metni hukukçuda. Gerçek cihaz turu (M16-59) birleştirmeden **sonra**, yalnız demo işletmede.
- **Taban ölçümü config PR 2'den önce** *[B-O6]*: (1) `"panel bcrypt work at capacity, refusing"` WARN sayısı (`workgate.go:279`) gün başına;
  (2) kesicinin saatlik gönderim hacmi (audit `*.emailed` / `*.sent` satırları saat başına); (3) owner bildirimi `undelivered` oranı. Log'lar 15
  gün yaşar (Q28 (b)) — ölçüm o pencerede ve sayı olarak kayda geçer.
- **SigNoz uyarıları** *[B-O5]*: `staff.signin.failed` oranı ve kapı retleri (`workgate.go:279`'un satırı) için iki kural (`tappa-signals`
  kanalı, Q28 (a)); eşik M16-52 ölçer; kurulum ops işidir.
- **Geri alma** *[A-27]*: config PR'ı → `git revert` + deploy (kullanıcı); env boşalınca yollar bağlanmaz, veri kalır. Yasal yayım →
  `/operator/legal`'dan önceki sürüm yeniden yayımlanır (sürümler saklanır — `op_read_legal_versions`).
- **3.2 yedeği** *[B-O12]*: hazır bir *Resolution Center* cevabı (`mobile/README.md`, M16-57) + kapı: ret sürerse kullanıcı kararı — (a) itiraz,
  (b) *Unlisted App Distribution* başvurusu, (c) Apple Business Manager özel uygulaması (işletme başına — SaaS için ağır).

## 3. Güvenlik değerlendirmesi

| Tehdit | Bu ADR'yle | Kalan |
|---|---|---|
| Parola doldurma / dağıtık tahmin | kaynak bütçeleri (`TryCharge`), ret listesi, K sabit iş, WorkGate, SigNoz uyarısı; ödül yalnız bir aktivasyon davetidir | küresel tavan ≈ 0,88 tahmin/sn, o sırada panel 503 (hesap); **tahminin sızdırdığı veri:** başarı `businessName`, lookup `employeeName`, `locationName`, `wifiSsid` (`appactivate.go:48-59`) — plaket olmadan. **ADR 0005 risk eki bunu da sayar** |
| Parola paylaşımı = buddy punching | bağ plakette; Q40 (a) gerçek çalışanın cihazını iptal eder; kişiye ve owner'a e-posta | risk 1 aynen |
| İşverenin eski davetiyle ikinci bağ (Y1) | tüketim bütün kardeşleri iptal eder; basım iki yönde emekliye ayırır | yok — [H52-c] |
| Saldırgan işletmelerle pencere dışına itme (Y2) | en eski önce + parolasız satır pencereye girmez | link isteğinde kurbanın en eski 3 işletmesi kapsanır; > 3 işletmeli kişi en yeni işletmesinde link alamaz (M16-50 ölçer) |
| Numaralandırma | tek cevap + bant; tek başarısızlık + K iş; silme sayfası panel girişinin cevabı | başarı başarıyı söyler |
| Posta kutusu ele geçirme | parola belirleme + bekleyen parola davetlerinin iptali; her olayda kişiye ve owner'a e-posta; kurtarma yolu §2.4.8 | adres bir doğrulayıcıdır (ADR 0049 bedel 3) |
| Kesicinin bildirimleri susturması (O3) | parola aktivasyonu owner'a saatlik özet; işletme işletme açılış; taban ölçümü | muaf gönderimler pencereyi doldurabilir — reddedilebilir posta (davet, sıfırlama) kapanır; panel sinyali dayanıklı |
| Müdür onayının uzaktan istismarı | onay listesinde *"via password sign-in — verify in person"* | müdür uyarıyı yok sayabilir |
| Silme talebinin kötüye kullanımı | yalnız aktif owner, kanıt, ad yazdırma; isteyene ve öteki owner'lara e-posta; 30 gün içinde operatör iptali | tek owner'ın talebi işletmeyi 30 günde siler (K-A) — owner'lar uyarılır |
| Demo işletmenin kötüye kullanımı | (Q55 (b)) yalnız çalışan kimliği; statik QR bağı yalnız işaretli işletmede | demo işletmede kalıcı satırlar |
| Ingress log'unda token (O1) | `/staff`, `/account-deletion` erişim log'suz | nginx hata log'u (40-ingress.yaml'ın adlandırdığı bedel) |
| Tenant'lar arası okuma | dar definer'lar, yalnız yeni sütunlar | `privCatalogAllowed` pinler |

## 4. Bedeller

1. Yeni bir kimlik bilgisi türü (paylaşılır, yeniden kullanılır); sızmış parola servisi yok, yerine gömülü ret listesi.
2. Adres bir doğrulayıcıdır. 3. Holding çalışanı işletme başına parola. 4. Çıkış kutusu dayanıklı değil.
5. WorkGate'in takası genişler; dağıtık tahmin panel girişini 503'e itebilir (§2.5). 6. Mekân NAT'ı. 7. Bellekteki kovalar pod sayısıyla.
8. Uygulama modu bir sunum sözleşmesi (kapalı `href` testi).
9. **Silme 30 günde tamamlanır** — Apple'ın sayfasına uygunluğu M16-50'de okunur, uymazsa **durulur**.
10. Q55 (b) seçilirse *"QR aktive etmez"* kuralına operatör bayrağına bağlı istisna.
11. **İnceleyici uygulamadan kayıtta KDV adımında durur** (gerçek AB KDV numarası ister) — not söyler, ret riski adlandırılır *[B-O10]*.
12. Davetli kişi parolalı girişten sonra 24 saatte tamamlamazsa işverenin daveti de gitmiştir — müdürden yeni davet (Q59 (b)).
13. Owner-çalışan yönetici adresiyle parola kullanamaz (§2.8).
14. Operatöre silme talebi e-postası yok; liste her iş günü okunur.
15. KDV'siz operatör işletmesi faturalama ekranlarında boş KDV çizer.

## 5. Kırmızı çizgiler

- **§4.1:** biyometri yok; anahtar kuralı ADR 0045 §2.3 aynen. **§4.2:** giriş, parola, kayıt, silme konum okumaz.
- **§4.3:** hiçbir yol `transactions`'ı değiştirmez; anonimleştirme `employees`/`admin_users` sütunlarındadır (Q13'ün önerdiği yol), kayıt
  satırları kalır ve `employee_id`'yi korur.
- **§4.4:** plaket bağı `sun.Verify`'dan geçer; statik QR bağı (Q55 (b)) davetin tek ifadeli tüketimi + kardeş iptali.
- **§4.5:** yeni tablolar §6 beşlisiyle; çözümleyiciler `tappa_resolver`; holding'de art arda `WithTenant`; operatör yalnız `op_*`.
- **§4.6:** silme talebi personeli durdurmaz; tamamlama (askı) sonrası dokunuşlar askının bugünkü kuralıyla karşılanır (ADR 0025).
- **§4.7 + §7:** parola, adres, token, kod hiçbir log/hata/`detail`'de yok; yeni HMAC anahtarı yalnız sırlarda; demo değerleri yalnız konsollarda.

## 6. Mağaza ve gizlilik sonuçları

- App Store: *Contact Info → Email Address* (App Functionality, bağlı, izleme yok); web görünümündeki kayıt formunun etiketi M16-50'nin
  okumasıyla. Play: *Personal info → Email address*; **tek silme URL'si** `https://taptime.mt/account-deletion`. Gizlilik bölüm 12: parola
  paragrafı, işletme kaydı, **işletme silme (30 gün, ne silinir, ne kalır, onay e-postası)**.

## 7. Karar verilmedi

- **Q55 … Q59** — her biri bir öneri taşır. **Kural:** önerilmeyen bir cevap seçilirse plan (ADR + Faz H kartları) yeniden yazılır ve
  **yeniden denetlenir**; o olana dek **M16-51 açılmaz**. Etki tablosu Faz H başlığındadır.
- *Sonra:* tarayıcıda parolayla giriş; uygulamada *Change password*; panelden *"Send app sign-in e-mail"*; iOS `webcredentials:`; global
  çalışan kimliği (reddedildi).

## 8. Uygulama

- **M16-50** kabul + ADR 0049 notu (*"YYYY-MM-DD: §2'nin link mekanizması ADR 0051'le yerini aldı; §2.4 bütçeleri parola linkine, §2.6
  görünürlüğü `origin='password'`'e, §2.7'nin iki kapısı ADR 0051 §2.8'e devredildi; 7 günlük bekleme devralınmadı; metin silinmez"*) ·
  **M16-51** parola şeması + link + sayfa (00118, 00119, 00120) · **M16-52** giriş + tüketimde kardeş iptali · **M16-53** ekranlar ·
  **M16-54** uygulama modu · **M16-55** silme talebi (00121) · **M16-56** yalnız Q55 (b) (00124) · **M16-57** demo + runbook (M16-39'un yerine)
  · **M16-58** beyanlar + gizlilik (M16-27/M16-41'in yayım adımlarının yerine) · **M16-59** kapanış · **M16-60** operatörden KDV'siz işletme
  (00122) · **M16-61** silmenin tamamlanması (00123).
- CLAUDE.md §5 cümleleri (M16-52, M16-56) kodla aynı commit'te; **metni M16-50'de kullanıcı AskUserQuestion ile onaylar** *[A-20]*.
