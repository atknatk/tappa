# ADR 0020 — Platform operatörü: ayrı kimlik, zorunlu TOTP, ayrı oturum ve ayrı host

- **Durum:** kabul edildi — kullanıcı kararı **D-B** (2026-09-24): *"(c) ayrı operatör
  kimliği — TOTP + `ops.taptime.mt` + `op_*` definer'lar; önceki 'B — allow-list
  genişlet' kararının yerine geçer"* ([m10-platform.md](../plan/m10-platform.md) §6).
  **Uygulama: yok.** Bu ADR yazıldığında (HEAD `f6f5a9b`) operatöre ait tek satır kod,
  tek tablo, tek rol yoktur (aşağıda ölçüldü); uygulama OP-5…OP-10.
- **Tarih:** 2026-09-26 · aynı gün **2. ve 3. tur:** güvenlik denetimlerinin RED'leri
  (izsiz okuma; bilet süresinin saati), üçüncü gözlerin bulguları ve **3. tur eki**
  (orkestratör kararı: enrollment ve kimlik bilgisi yazımı definer'da) ve **4. tur**
  (iki merceğin ORTA/DÜŞÜK bulguları) işlendi (sapma listesi:
  [m10-platform.md](../plan/m10-platform.md) → OP-4 kart düzeltmesi).
- **Bağlam:** [M10 Akış A](../plan/m10-platform.md) §3, görev OP-4 · Olay A-0 (aynı
  dosya §0)
- **Yerine geçtiği:** [ADR 0016](0016-tenant-kapsamsiz-operator-icerigi.md) §5
  (tümü), §2'nin "ikinci giriş" gerekçesi, §2b'nin "tek cevap izin listesi" cümlesi ·
  M7-06'nın *"yeni giriş yok / süper admin icat edilmemeli"* kararı · M9-08 için alınan
  *"B — mevcut allow-list modelini genişlet"* kararı. Madde madde:
  [Yerine geçilen metinler](#yerine-geçilen-metinler--madde-madde).
- **İlgili:** [ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md) (bu ADR'nin
  veritabanı yarısı — tenant sınırını aşmanın tek yolu) ·
  [ADR 0002](0002-tenant-baglami-ve-rls.md) md.6, md.7 ·
  [ADR 0013](0013-kayit-sihirbazi-ve-ilk-gelen-sirasi.md) (herkese açık kayıt) ·
  [ADR 0014](0014-parola-digestinin-tabani-islenebilirlik.md) (bcrypt) ·
  [ADR 0015](0015-sifirlama-tokeni-tek-gecislik-yetkidir.md) ·
  [ADR 0019](0019-parola-min-8.md) (tenant parolası ≥8 — **değişmez**) ·
  CLAUDE.md §4.5, §4.7, §7, §10

## Neden bir ADR — ve neden iki

CLAUDE.md §10: *"Şema, karar motoru veya güvenlik sınırı değiştiyse ADR yaz."* Bu karar
üçünü birden değiştirir: yeni bir kimlik yüzeyi, yeni tablolar ve ürünün ilk bilinçli
tenant-ötesi erişimi. Ayrıca ADR 0016 §5 **yazılı bir karardı**; bir kararın yerine
geçmek sessizce yapılamaz, neyin düştüğü ve neyin ayakta kaldığı adıyla söylenir.

İki ADR'ye bölünmesinin ölçütü kapsam değil **denetim merceğidir** (agent-brief.md,
*"Bir görevi A/B fazına bölmek"*): **bu ADR** kimliği anlatır — kim, nasıl girer,
nereden girer, ne iz bırakır. [ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md)
§4.5'in aşıldığı **tek yeri** anlatır — roller, `op_*` sözleşmesi, hangi sütunların
asla okunmadığı. İkisi farklı saldırılarla denetlenir.

## Bağlam — bugün (ölçüldü, HEAD `f6f5a9b`)

| Olgu | Kaynak / ölçüm |
|---|---|
| Operatör gücü = env'deki bir **`admin_users.id` izin listesi** | `internal/config/config.go` `OperatorAdminIDs` + `operatorAdminIDs` ayrıştırıcısı; env `TAPPA_OPERATOR_ADMIN_IDS` |
| Üretim listesinde **tek bir id** var — Olay A-0'a göre Kebab Factory Ltd. owner'ı | `deploy/k8s/05-config.yaml` (id bir sır değil — ADR 0016 §5 — ama burada yazılmadı, gerek yok) |
| Kapı: `mayPublishLegal`; sekme: `TabLegal` + `PanelSection.OperatorOnly` + `PanelChrome.Operator`; tek ekran `/admin/legal` | `internal/handler/legaladmin.go`, `web/templates/pages/adminview.go`, `internal/handler/review.go` |
| Operatör **bir müşteri oturumuyla** girer: aynı `tappa_admin_session` çerezi, aynı çözümleyici | `internal/adminauth/cookie.go` |
| **Sunucu tarafı oturum süresi yok**: 12 saatlik `Max-Age` bir tarayıcı ipucu; `admin_sessions` sütunları `id, tenant_id, admin_user_id, token_hash, created_at, last_used_at, revoked_at` — `expires_at` yok | `cookie.go` `cookieMaxAgeSeconds` yorumu; katalog sorgusu |
| **MFA yok** — `totp`/`otpauth` kod ağacında (`internal`, `cmd`, `db`, `web`) 0 isabet | `grep -rli` |
| `/operator` rotası yok, host kapısı yok | `grep` — `hostGate` tanımı 0, `"/operator` 0 |
| Operatörün yayın audit'i **çağıranın kendi tenant'ının** `audit_log`'una yazılıyor (yani KF'nin) | ADR 0016 §2 |
| `tappa_app` `legal_documents`'a **yazabiliyor**: sütun düzeyi `INSERT (slug, body, published_by)` | `pg_attribute.attacl`; `has_any_column_privilege(…,'INSERT') = t` |
| Rol kümesi: `tappa_app` (NOSUPERUSER, NOBYPASSRLS), `tappa_owner` (superuser), `tappa_resolver` (NOLOGIN, BYPASSRLS). Operatör rolü, `platform_*` tablosu, `operator_audit_log` **yok** | `pg_roles`, `pg_class` |
| Ingress host'ları `taptime.mt`, `www.taptime.mt`, `tappa.everva.com.tr` — operatör host'u yok | `deploy/k8s/40-ingress.yaml` |
| `admin_users` e-postası yalnız **tenant içinde** tekil (kısmi `UNIQUE (tenant_id, email)`), kayıt herkese açık | katalog; ADR 0013, ADR 0016 §5 |

**Olay A-0 (2026-09-24):** izin listesindeki tek hesabın canlı panel parolası bir borç
notunun **içinde** `state.md`'ye yazılmış ve **PUBLIC** depoya pushlanmıştı. Triage
yetkisiz kullanım bulmadı; ama o hesap `/admin/legal` üzerinden Taptime'ın herkese
açık gizlilik/künye metinlerini değiştirebiliyordu. Parolanın kendisini döndürmek F0-1'dir
(kullanıcı işi, bu ADR'nin kapsamı dışında). Bu ADR **sınıfı** kapatır: *platform gücü
bir restoran hesabına bağlı olmamalı* (m10-platform.md §1).

## Neden şimdi — dört gerekçe (m10-platform.md §3)

1. **M9-08'in 4. kabul kriteri** — *"bir müşteri oturumu oraya hiçbir koşulda
   giremiyor"* — izin listesiyle **yapısal olarak** karşılanamaz: izin listesinde
   operatör oturumu **bir müşteri oturumudur**. Kriter yalnız handler başına bir kapıyla
   "karşılanır", yani her yeni ekranda yeniden doğru yazılmak zorunda olan bir cümleyle.
2. **ADR 0016 §2, izin listesinin genişlemesinin gerektirdiği deseni YASAKLIYOR**:
   *"müşteri tenant'ındaki bir oturumla ulaşılabilen bir handler"da*
   `db.WithTenant(ctx, <çağıranınki DEĞİL>, …)`. Tenant listesi, faturalama, VAT ve
   plaket ekranlarının her biri tam olarak bu şekli ister.
3. **A-0**: platform gücü bir restoran hesabına bağlıyken o hesabın parolası sızınca
   platform açığı olur; MFA yok, sunucu tarafı süre yok, audit müşterinin tenant'ında.
4. **Kapsam farkı**: ADR 0016 §5'in *"Yeni giriş yok"* gerekçesi **dört yasal
   belge** içindi (*"Dört belge bunu haklı çıkarmaz"*). M9-08'in kapsamı (tenant
   listesi/arama/askı, faturalama, VAT yeniden doğrulama, plaket envanteri) tenant
   sınırını **aşan** işlevlerdir; gerekçe o kapsama uzanmaz.

## Karar

### 1. Kimlik — `platform_admins`: müşteri tablolarından ayrı, tenant kapsamsız

- **`email citext`, GLOBAL UNIQUE.** ADR 0016 §5'in ölçtüğü kırılma — *"bir izin
  listesi ancak anahtarının tekilliği kadar değerlidir"* — burada iki yapısal özellikle
  kapanır: anahtar **global** tekildir ve bu tabloya **hiçbir herkese açık yol satır
  yazamaz** (§6: yalnız `tappa_owner` INSERT eder). Bir `admin_users` satırının aynı
  adresi taşıması önemsizdir: ayrı tablo, ayrı form, ayrı host, ayrı çözümleme.
- **Parola:** bcrypt **cost 12** (`internal/adminauth` `Cost = 12` emsali), en az **14
  rune** (K10), en çok **72 bayt** (bcrypt'in kendi tavanı —
  `internal/adminauth/password.go` `MaxPasswordBytes = 72`). Plan yalnız bir
  taban koyar; kompozisyon kuralı eklenmez (ADR 0019'un gerekçesi aynen geçerli).
  **Tenant tabanı 8 kalır**; ADR 0019 değişmez.
- **`totp_secret_sealed`:** TOTP sırrı **AES-256-GCM** ile, **ayrı** bir KEK altında
  (`TAPPA_OPERATOR_TOTP_KEK`) sarmalanır; düz sır DB'de asla. 🔴
  **`TAPPA_OPERATOR_TOTP_KEK`, `TAPPA_TAG_KEK` ile aynı olamaz** — aynıysa süreç
  açılışta reddeder; diğer anahtarlarla eşitlik de aynı şekilde reddedilir (OP-7
  kabulü; `config.go` `keySeparation` emsali).
- **Kriptografinin yeri — orkestratör kararı (2026-09-26), CLAUDE.md DEĞİŞMEZ.**
  Ölçüm: **üretim kodunda** `crypto/aes` ve `crypto/cipher` yalnız `internal/sun`'da
  import ediliyor (test dosyaları da import ediyor: `cmd/rotatekek/main_test.go`,
  `internal/encode/chip_test.go`, `internal/sun/cmac_test.go`); `crypto/hmac` ve
  `crypto/sha256` ise zaten `internal/adminauth`, `internal/handler`,
  `internal/invite` ve `internal/session`'da. Yani CLAUDE.md §3'ün *"Kriptografi SADECE
  burada"* kuralı bugün fiilen **AES / blok şifre** içindir; HMAC dışarıdadır. Buna göre:
  1. **Mühür `internal/sun`'da, yeni genel bir `Seal` / `Open` çiftiyle:** AES-256-GCM,
     her çağrıda rastgele nonce (`Wrap`'ın ilkesi). Mevcut `Wrap`'a sığdırma kolu
     **kaldırıldı**: `internal/sun/keys.go:98-103` `Wrap`'ın 7 baytlık `uid` ve 16
     baytlık anahtar dışındaki her girdiyi **reddettiğini** gösteriyor (üçüncü göz
     ölçtü; kaynak aynı şeyi söylüyor). AES `internal/sun` dışına çıkmaz.
  2. **AAD = `platform_admins.id`'nin 16 baytı, kırpılmadan** — zarf kendi satırına
     bağlanır; başka bir satıra taşınan zarf açılmaz (ADR 0003 md.4'ün `uid`-AAD
     emsali).
  3. **TOTP sırrı 160 bit** (RFC 4226 §4'ün önerdiği boy; alt sınır 128).
  4. **TOTP'nin HMAC-SHA1 hesabı** (RFC 6238 / RFC 4226) `internal/operatorauth`'ta
     yapılır — mevcut HMAC kullanımıyla (`adminauth`, `session`, `invite`) aynı sınıf.
- **`totp_last_step` — tekrar koruması, CLAUDE.md §4.4'ün aynası.** Kabul edilen kodun
  adımı tek ifadeyle ilerletilir:
  `UPDATE platform_admins SET totp_last_step = $2 WHERE id = $1 AND totp_last_step < $2`
  — **0 satır = ret**. Oku-sonra-yaz yok; aynı kodla N eşzamanlı deneme tam 1 başarı
  (OP-6 kabulü). Bu ifade `op_open_session`'ın **içindedir** ve oturumu o güncellemenin
  döndürdüğü satırdan doğurur — tekrar edilen bir kod oturum açamaz (§3). İlk değerini
  `op_complete_enrollment` yazar (enrollment'ın ilk kodunun adımı).
  🔴 **Adım duvar saatine bağlıdır:** aynı ifade `$2`'nin `cur - 1 … cur + 1` içinde
  olmasını şart koşar (`cur = floor(epoch(clock_timestamp()) / 30)`). Bağsız hâli bir
  kalıcı kilit ilkeliydi — güvenlik denetimi ölçtü: `bigint` üst sınırı kabul edildi,
  ardından her meşru adım reddedildi (ADR 0021 §1, ölçüm tablosuyla).
- **`tappa_operator` `platform_admins`'e HİÇBİR ŞEY yazamaz** (`INSERT`, `UPDATE`,
  `DELETE` — 3. tur eki, orkestratör kararı). Enrollment, TOTP adımı, son giriş ve kilit
  sayacı definer'lardan geçer (`op_complete_enrollment`, `op_open_session`,
  `op_record_auth_event`); satırı yaratmak, sıfırlamak ve kapatmak `tappa_owner`'ın
  `opadmin` SQL'idir (§6). Giriş için digest'i ve zarfı **okur** (ADR 0021 sınır 11).
  🔴 **Sütun `NOT NULL` ve başlangıç değeri gerçek her adımdan küçük olmak zorunda.**
  Ölçüldü (`BEGIN … ROLLBACK`): sütun `NULL` iken bu ifade **0 satır** döndürür
  (`NULL < x` bilinmez) — yani hiç kod kullanmamış bir operatörün **ilk** kodu
  reddedilirdi.
- **`status` ∈ `pending | active | disabled`**, ve bir CHECK: `active` ⇒ parola
  digest'i **ve** TOTP zarfı dolu. MFA'sız bir operatör şema düzeyinde "aktif" olamaz.
- **`admin_users.role`'e dokunulmaz.** ADR 0016 §5'in *"yeni rol yok"* maddesi
  **ayakta**: o kapalı sözlüğü policy motoru okur (`actor:role`); operatör ayrı bir
  tabloda yaşar, müşteri rol sözlüğüne üçüncü bir değer girmez.
- **Tablo boşsa kimse giremez.** *"Boş liste = kimse"* (ADR 0016 §5, M9-08 kabul 3)
  ilkesinin yeni evi budur; env'den tohumlama ve kurulum sayfası reddedildi (§6).

### 2. Oturum — `platform_sessions` ve çerez `__Host-taptime_op`

- **Sunucu tarafı süre, doğuştan:** mutlak **8 saat** (oturumun doğuşundan), boşta
  **30 dakika** (son kullanımdan). Bugünkü panel oturumunun eksiği (yukarıdaki tablo)
  burada hiç doğmaz.
- **`mfa_verified_at`:** boş olan oturum hiçbir operatör rotasında ve hiçbir `op_*`
  çağrısında geçerli değildir.
- **`revoked_at`:** çıkış (`op_close_session`), `disable`, `reset-mfa`.
- **Oturumun bütün hayatı definer'lardadır** (ADR 0021 §1): enrollment sonunda
  `op_complete_enrollment`, girişte `op_open_session` onu doğurur ve köken satırını
  **aynı ifadede** yazar (§3); `op_touch_session` her istekte yaşatır;
  `op_close_session` bitirir ve çıkış satırını aynı ifadede yazar. `tappa_operator`
  `platform_sessions`'ta **hiçbir fiil** tutmaz ve `token_hash`'i **göremez** — güvenlik
  denetimi, touch için o sütunda `SELECT` verilen rolün **bütün canlı hash'leri
  listelediğini** ölçtü.
- **Touch tek ifade, fail-closed, duvar saatiyle:** geçerlilik (mutlak süre + boşta süre +
  MFA + iptal + operatörün `status = active` olması) ve `last_used_at` güncellemesi
  **aynı** ifadededir; **0 satır = oturum yok**. Oku-sonra-karar-ver yok (§4.4 dersi).
  Yüklem **tek** tanımdır, `op_touch_session` (ADR 0021 §2 i): Go'nun oturum kapısı ve
  her `op_*` onu çağırır. 🔴 Süreler **`clock_timestamp()`** ile karşılaştırılır, `now()`
  ile değil: `now()` transaction başlangıcında donar ve açık tutulan bir transaction'da
  süresi dolmuş bir şeyi "canlı" gösterir (ölçüldü — ADR 0021 §2 vii).
- **Token:** 256 bit rastgele (panel token'ının `tokenBytes = 32` emsali); DB'de yalnız
  HMAC'i, çerezde ham değer. 🔴 **HMAC anahtarı KENDİ değişkenidir:
  `TAPPA_OPERATOR_TOKEN_HMAC_KEY`** — session anahtarından bir etiketle türetilmez.
  Gerekçe `internal/adminauth/token.go`'nun kendi uyarısıdır (`adminTokenKeyLabel`
  yorumu): türetilmiş anahtar kaynağından bağımsız değildir ve *"a future credential
  that needs real independence should get its own variable rather than another label
  here"*. Operatör kimliği tam olarak o bağımsızlığı ister. Anahtar diğer her
  anahtardan farklı olmak zorundadır — eşitse süreç açılışta reddeder (`keySeparation`
  emsali); `TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest` onu manifestte
  zorlar; kullanıcının `tappa-secrets`'a ekleyeceği adlar arasındadır (Sonuçlar). (Uyarı
  `internal/adminauth/token.go:82-87`'de.) Tipin redaksiyon arayüzleri **farklı bir yer
  tutucu** basar — `token.go:59-62`'nin gerekçesiyle: bir yer tutucuyu grep'leyen bir
  sızıntı testi, **ötekinin** sızan değerini görmeden geçer; iki ayrı dize iki sızıntı-test
  takımının **birbirine kefil olmasını** engeller.
- **Çerez `__Host-taptime_op`:** `Secure`, `HttpOnly`, `SameSite=Strict`, `Domain` yok.
  `__Host-` öneki tarayıcıya `Secure` + `Path=/` + Domain'sizliği **zorlatır** — çerez
  yalnız operatör host'una gider. Panel çerezi (`tappa_admin_session`, `Path=/admin`) ve
  çalışan çerezi ana host'a bağlıdır. Ayrı ad, ayrı tablo, ayrı HMAC anahtarı, ayrı
  çözümleme: bir müşteri çerezinin değeri operatör çerez adıyla gönderilse de hiçbir
  şeye çözülmez, ve tersi (OP-8 kabulü, iki yönde).

### 3. Giriş akışı

1. `POST /operator/login` — e-posta + parola. **Bilinmeyen e-postada da sabit bir
   bcrypt ödenir** (`internal/adminauth` `dummy` digest emsali): bilinmeyen adres,
   yanlış parola, `disabled` hesap ve **`pending`** (enrollment'ı bitmemiş) hesap **aynı
   yanıtı ve aynı süreyi** alır — `pending` bu kümede olmasaydı bir *"bekleyen operatör
   var"* kehaneti kalırdı. Başarı bir
   oturum **değil**, kısa ömürlü, imzalı bir **ara çerez** üretir — yalnız TOTP adımına
   izin verir.
2. `POST /operator/login/totp` — TOTP Go'da doğrulanır → `op_open_session(hesap,
   oturum hash'i, kabul edilen adım)` **tek ifadede** adımı ilerletir (tekrar koruması),
   son girişi yazar, kilit sayacını sıfırlar, oturum satırını `mfa_verified_at` dolu
   doğurur ve **köken satırını (`login`)** yazar; adım eskiyse 0 satır → ret, oturum yok.
   Parola ve TOTP doğrulaması süreçtedir (TOTP
   KEK'i orada); definer bunu doğrulayamaz, çağırana güvenir — bu **yeni bir güç
   eklemez** (DSN sahibinin oturum basabilmesi zaten risk 4), yalnız **iz** ekler: var
   olan her oturumun commit edilmiş bir köken satırı vardır (ADR 0021 §1).

- **TOTP yalnız stdlib ile:** RFC 6238, HMAC-SHA1, 6 hane, 30 sn adım, **±1 adım**
  pencere. Yeni modül yok (`go.mod` bugün beş doğrudan bağımlılık; TOTP için ekleme
  gerekmez — CLAUDE.md §1 *"stdlib > küçük kütüphane"*). **QR kütüphanesi yok**:
  enrollment ekranı base32 sırrı ve `otpauth://` URI'sini metin olarak gösterir.
- **Sınırlar:** adres (flood) · iş (attempt) · hesap (account) limiter'ları — panel
  girişindeki üçlünün emsali (`adminFloodLimit`, `adminLoginWorkLimit`,
  `adminAccountLimit`) — ve **N hatalı TOTP'ta hesap kilidi + audit satırı**.
- **Oturum doğmadan önceki başarısızlıkların audit'i** dar, oturumsuz bir definer'dan
  geçer: `op_record_auth_event` — kapalı tür kümesi (`login_failed`, `unknown_email`,
  `totp_failed`, `locked`, `enrollment_failed`), yalnız başarısızlık, aktör iddiası yok,
  **adres de adresin hash'i de saklanmaz** (eşleşen hesabın id'si *hedef hesap* olarak
  yazılır), `RETURNS void` (ADR 0021 §1). 🔴 **İnternetten tetiklenen bir yazma kapısıdır
  ve sınırlıdır (normatif):** oturum öncesi satırlar **IP'den bağımsız, süreç genelinde bir
  tavanla** sınırlanır (sayısı OP-6/OP-8); **limiter'ın reddettiği istek satır yazmaz**;
  ve **TOTP kilidi audit satırlarından türetilmez** — kilit kararı satırları saymaz,
  `platform_admins` üzerindeki hesabın kendi sayacını okur — ve bu okuma
  `op_open_session`'ın **aynı `UPDATE`'inin** `WHERE`'indedir (eşik ve varsa pencere
  `clock_timestamp()` ile; oku-sonra-karar Go'da kalmaz). Sayacı `op_record_auth_event`
  `totp_failed` satırını yazdığı **aynı ifadede** artırır, `op_open_session` başarıda
  sıfırlar; internetten gelen biri `totp_failed`'ı ancak parola adımını geçtikten sonra
  ve limiter'ın izin verdiği kadar üretir. DSN sahibine karşı kilit korunamaz (sayaca
  giden her yol onun çağırabildiği bir definer'dır — ADR 0021 sınır 7). Gerekçe deponun
  kendi dersidir: panel bugün bilinmeyen e-postayı
  veritabanına değil yalnız süreç log'una yazıyor (`internal/handler/adminlogin.go:1232`)
  ve aynı dosya *"an unbounded row here would be a write primitive into an append-only
  table"* diyor (`adminlogin.go:1300-1301`).
- **Kurtarma kodu YOK** (K3). Cihaz kaybında tek yol `opadmin reset-mfa` (§6): TOTP
  zarfı silinir, kilit sayacı sıfırlanır, durum `pending`, bütün oturumlar iptal, yeni
  enrollment token'ı ve id'li link.
- **Enrollment:** `/operator/enroll` — `opadmin create`'in ürettiği link (hesabın id'si +
  tek kullanımlık token, TTL **30 dk**) ile açılır; parola belirlenir, TOTP sırrı
  gösterilir, **ilk kod doğrulanınca** `active` olur ve ilk oturum açılır.
  - **Token 256 bit rastgele; DB'de ANAHTARSIZ SHA-256'sı.** CLI sunucu anahtarı
    taşımaz (`cmd/rotatekek` gibi sır tutmayan bir filtre kalır). Anahtarsız hash
    burada yeterlidir çünkü girdi yüksek entropilidir: 256 bitlik rastgele bir değerin
    ön-görüntüsü aranamaz; anahtarlı hash'in katkısı düşük entropili girdilerdedir.
  - **Tamamlama bir definer'dır: `op_complete_enrollment`** (3. tur eki, orkestratör
    kararı; ADR 0021 §1). **Tek bir koşullu ifadede** (ADR 0015'in tek geçişlik tüketim
    emsali): ham token'ı içeride hash'ler; *id + hash eşleşiyor*, *`pending`*,
    *kullanılmamış* ve *süresi geçmemiş* (`clock_timestamp()` — ADR 0021 §2 vii)
    koşullarıyla token'ı tüketir; parola digest'ini ve mühürlü TOTP sırrını yazar,
    `totp_last_step`'e ilk kodun adımını yazar, durumu `active` yapar, oturumu açar ve
    köken satırını (`enrollment`) yazar. **0 satır = ret**, nedeni ayrılmadan.
    Oku-sonra-tüket yok.
  - **Go'da yapılanlar ve çağrıya verilenler:** Go 160 bitlik sırrı üretir ve gösterir;
    ilk kodu RFC 6238 ±1 ile **doğrular** ve kabul edilen **adımı** bulur; sırrı
    `internal/sun` `Seal` ile, AAD = linkteki hesap id'si, mühürler; parolayı bcrypt
    cost 12 ile digest'ler; bunları ham token ve yeni oturumun hash'iyle birlikte
    `op_complete_enrollment`'a verir. Definer enrollment'ın hak edilip edilmediğini
    doğrulayamaz; ama **ham token** ister, yani onu çağırmak linki gerektirir. Kabul
    edilen adımın `totp_last_step` olarak yazılması, enrollment kodunun ilk girişte
    **tekrar oynatılamamasını** sağlar; adım burada da `cur ± 1` ile sınırlıdır.
  - **Kimliksiz son adımın bedeli:** `POST /operator/enroll` token veritabanında
    doğrulanmadan önce bir cost-12 bcrypt ve bir `Seal` öder; çare ADR 0015 emsaliyle
    adres başına ve süreç geneli bir **oran sınırıdır** (ADR 0021 sınır 12; sayılar OP-8).

### 4. Rota ve host

- **Rotalar:** `/operator/login`, `/operator/login/totp`, `/operator/enroll`,
  `/operator/logout`, `/operator` (tenant listesi), `/operator/tenants/{id}`,
  `/operator/legal`, `/operator/billing`, `/operator/plaques`, `/operator/audit`.
- **Host kapısı:** `TAPPA_OPERATOR_HOST` (D-B: `ops.taptime.mt`). Başka bir host'tan
  gelen `/operator/*` isteği **404** alır — ana host'ta (`taptime.mt/operator`) operatör
  yüzeyi yokmuş gibi görünür.
- **Zincir, bu sırayla:** `hostGate → floodGate → sameOriginGate (operatör origin'i) →
  requireOperator → sessionGate`. Sıra yük taşır ve `internal/handler/adminlogin.go`'nun
  ölçtüğü sıradır: kimliksiz ret'ler çözümleyiciden **önce** (ucuz), oturum bütçesi
  kimlikten **sonra** (anahtarı oturumdur). `hostGate` ve `requireOperator` bugün
  yoktur; `floodGate`/`sameOriginGate`/`sessionGate` panelin emsalleridir.
- 🔴 **`SameSite` burada yetmez, `sameOriginGate` ZORUNLU.** `ops.taptime.mt` ve
  `taptime.mt` **aynı site**dir (kayıtlı alan adı `taptime.mt`). SameSite yalnız
  site-ötesi istekleri keser; ana host'ta bir XSS ya da bir alt alan adı ele geçirmesi
  operatör host'una "same-site" istek atar. Aynı sınıf panelde kayda geçti
  (`ProtectWriting` yorumu, M6-04): yorum, `SameSite=Lax`'ın gerçek bir site-ötesi
  sayfayı durdurduğunu ama aynı sitenin başka bir origin'ini durdurmadığını yazar;
  orada **ölçülen** şey, origin kapısı çözümleyiciden sonra durduğunda cross-origin bir
  POST'un çözümleyiciyi çalıştırmasıydı (çözümleyici çağrısı 1; kapı öne alınınca 0).
  Operatör yüzeyinde origin karşılaştırması **operatör host'unun** origin'iyle,
  çözümleyiciden **önce** yapılır.
- **Durum kodları:** oturumsuz → **303** `/operator/login` · bilinmeyen rota **404** ·
  `TAPPA_OPERATOR_DATABASE_URL` yoksa `/operator/*` **503** + adı konmuş bir hata;
  müşteri paneli etkilenmez (OP-7 kabulü).
- **Sonuç:** müşteri oturumunun operatör yüzeyine girmesi **yapısal olarak**
  imkânsızdır — operatör çerezi yoksa 303, yanlış host'ta 404. Bu, M9-08 kabul 4'ün
  handler başına bir kapıya değil yapıya dayanan karşılığıdır.

### 5. Audit

- **`operator_audit_log`** — `tenant_id` **taşımaz** (hiçbir tenant'a ait değildir;
  ADR 0016 §1 kalıbı: `-- redline: no-tenant-scope(…)` muafiyeti, R5 her koşuda WARN
  basar). **Append-only**, `audit_log` / `legal_documents` ailesinin şekliyle: UPDATE ve
  DELETE hiçbir role verilmez, `tappa_forbid_mutation()` satır trigger'ı ve 00021'in
  tablo-boşaltma trigger'ı (bugün `tappa_forbid_mutation` kullanan her tabloda ikisi
  birlikte var — katalogda ölçüldü) → değişiklik `tappa_owner` için de hata verir (OP-5
  kabulü). ⚠️ **Mutlak değil:** bir superuser trigger'ı `DISABLE` edebilir ya da
  tabloyu `DROP` edebilir — 00005'in kendi ifadesiyle *"defense-in-depth, mutlak
  degil"* (00005:22-23), 00021'inkiyle *"defence in depth … not an absolute"*
  (00021:79-82).
- **HER eylemi tutar:** başarılı ve başarısız girişler (bilinmeyen e-posta dahil —
  **adres yazılmadan**, M7-06'nın *"The address is NOT logged"* pratiği ve R7b),
  enrollment, TOTP kilitleri, yayınlar, ve tenant verisinin **okunması** (liste +
  detay). Satır, dokunulan tenant'ı (varsa) adlandırır — M9-08 kabul 1'in *"hangi tenant
  adına"* yarısı; sütun şekli OP-5.
- **Her audit satırı bir definer'dan gelir; `tappa_operator` `operator_audit_log`'a
  doğrudan yazmaz.** Oturum öncesi başarısızlıklar `op_record_auth_event`'ten; oturumun
  doğuşu `op_open_session`'ın (`login`) ve `op_complete_enrollment`'ın (`enrollment`)
  köken satırından; çıkış
  `op_close_session`'dan; geri kalan her şey oturum taşıyan bir `op_*`'tan (ADR 0021).
- **Her satırın zorunlu içeriği (normatif; sütun adları OP-5):** oturum kimliği
  (oturumlu satırlarda), **tür** ve **hedef** (tenant ve, okumalarda, hedef kapsamı +
  içeriksiz sayfa bilgisi). 🔴 **Arama terimi ve keyset imleci audit satırına YAZILMAZ — ne
  ham ne hash'i** (imleç sonuç satırından türer, yani sonucu ve kişisel veriyi taşır)
  (anahtarsız hash sözlükle geri çevrilir; §3'ün e-posta gerekçesi); terim, saklanmayan
  ham biletle birlikte bilet hash'inin **içinde** bağlanır (ADR 0021 §2 v 1, ölçümüyle).
  ADR 0021'in sınır 1 ve 4 savunusu bu değerlere dayanır.
- **Zaman damgası:** `operator_audit_log`'un zaman sütunu `DEFAULT clock_timestamp()`'tir
  ve INSERT listesinde yoktur; K6'nın tenant `audit_log` satırında `at` açıkça
  `clock_timestamp()` ile yazılır (00005'in `DEFAULT now()`'ı açık tutulan bir
  transaction'da satırı geriye tarihler — ölçüldü, ADR 0021 §2 vii).
- 🔴 **Okuma, audit'i commit edilmeden veri döndürmez.** Tek aşamalı bir okuma izsiz
  okumaya izin veriyordu (güvenlik denetimi: `SAVEPOINT` → çağrı → `ROLLBACK TO` = veri
  elde, audit 0 satır). Okumalar **iki aşamalıdır**: `op_begin_read` audit satırını
  yazar ve **commit edilir**, sonra ayrı bir transaction'da `op_read_*` bileti tüketip
  veriyi döndürür; bilet yaratıldığı transaction'da kullanılamaz, ömrü **en çok 60 sn**'dir
  ve duvar saatiyle karşılaştırılır (ADR 0021 §2 v, vii — ölçüm tablolarıyla). Bilet
  **tek kullanımlıktır, bir sınırla:** geri alınan bir okuma onu tüketilmemiş hâle
  döndürür ve ömrü içinde (≤ 60 sn) yeniden kullandırır — ADR 0021 sınır 4. M9-08 kabul
  2'nin aslı — *"sessiz bir çapraz-tenant okuma yok"* — `tappa_operator` DSN'ini tutan
  birine karşı da böyle tutar.
- **Tenant'ı değiştiren eylem ayrıca** hedef tenant'ın `audit_log`'una, **aynı
  transaction'da** (K6): `actor_id` = `platform_admins.id`, `detail.actor_kind =
  "operator"`. Migration gerekmez — `audit_log.actor_id` FK taşımaz (ölçüldü: tabloda
  tek FK `tenant_id → tenants`). **Okumalar tenant audit'ine yazılmaz** (K6).
- **Detail'e ASLA:** CLAUDE.md §7'nin *"asla loglanmaz"* listesinin **tamamı** —
  oturum token'ı, CMAC, AES anahtarı, davet kodu, tam GPS koordinatı — ve operatöre özgü
  olarak: TOTP sırrı ya da zarfı, TOTP kodu (±1 penceresinde canlı bir kimlik
  bilgisidir), oturum/enrollment token'ı ya da hash'i, okuma bileti, parola ya da
  digest'i. Operatör **id** ile anılır, adresle değil. (Tenant-ötesi okumaların GPS/IP
  sütunları: ADR 0021 sınır 9.)
- 🔴 **Okuma bileti, TOTP kodu ve enrollment token'ı HİÇBİR YERDE loglanmaz** — süreç
  log'u (`slog`), hata mesajı, audit `detail`'i, **ingress'in erişim log'u**; ham hâlleri
  de hash'leri de. CLAUDE.md
  §7'nin listesinde bu üçü yok; bu ADR onları ekler, CLAUDE.md güncellemesi açık iştir
  (Sonuçlar).

### 6. Bootstrap — `cmd/opadmin`

- **Filtre CLI, `cmd/rotatekek` emsali:** bağlantı yok, DSN yok, sürücü yok; SQL
  üretir, operatör onu `psql` ile **`tappa_owner`** olarak uygular. Alt komutlar:
  `create --email --name` (pending satır + enrollment token'ın anahtarsız **SHA-256**
  hash'i — CLI yerelde hesaplar, sunucu anahtarı taşımaz; ham token stderr'e **bir
  kez**, TTL 30 dk, SQL çıktısında **asla**) · `reset-mfa` · `disable`.
- **`reset-mfa` tanımı:** TOTP zarfını siler, kilit sayacını sıfırlar, hesabı
  `pending`'e döndürür, bütün oturumlarını iptal eder ve **yeni** bir enrollment token'ı
  ile id'li link üretir (`create` ile aynı teslim kuralları). OP-9'da açık iş olarak
  adlandırıldı.
- **`create` hesabın id'sini kendisi üretir** (`crypto/rand` uuid), SQL'e yazar ve
  enrollment linkine token'la birlikte koyar: TOTP zarfının AAD'si `platform_admins.id`
  olduğu için Go id'yi mühürlemeden **önce** bilmek zorundadır, ve `tappa_operator`
  enrollment hash'ini okuyamaz (ADR 0021 §1). Id bir sır değildir; `op_complete_enrollment`
  id'yi ve token hash'ini **birlikte** eşleştirir.
- 🔴 **Token sorgu dizgisinde TAŞINMAZ.** Ingress erişim log'u URL'yi yazar; ürünün kendi
  `AccessLog`'u tam bu yüzden URL'yi değil rota desenini yazıyor
  (`internal/httpx/requestlog.go:399-406` — *"This product's two most sensitive credentials
  travel in the QUERY STRING"*). Token ya **fragment**'ta (sunucuya hiç gitmez; sayfa onu
  gövdede gönderir) ya da **yol parçasında** taşınır — ikincisi ingress yolu log'luyorsa
  aynı sorunu taşır. Hangisinin seçildiği ve ingress'in ne log'ladığı OP-9'da ölçülür.
- **`platform_admins`'e yalnız `tappa_owner` INSERT eder** (ADR 0021 §1). HTTP'den ya
  da uygulama rollerinden operatör yaratmak imkânsızdır; tablo boşsa kimse giremez.
- **Reddedilen:** env'den tohumlama — bir kimliği bir dağıtımın yan etkisi yapar ve bir
  kimlik bilgisini bir yapılandırma dosyasına taşır (A-0'ın sınıfı: sır bir kayıt
  dosyasında). Herkese açık kurulum sayfası — tablo boşken herkese açık bir yazma yolu,
  yarışı kazananı platform operatörü yapar.

### 7. Yasal metinlerin taşınması ve izin listesinin emekliye ayrılması (OP-10)

- `/operator/legal` → `op_publish_legal(session, slug, body)` (ADR 0021 sözleşmesi).
- **`REVOKE INSERT ON legal_documents FROM tappa_app`** — ADR 0016 §2b'nin *"yazma
  tarafında DB derinliği yok"* borcu kapanır: tek yazma yolu `tappa_opdefiner`'ın
  fonksiyonu olur.
  🔴 **Ölçüm sözleşmesi:** `has_table_privilege('tappa_app','legal_documents','INSERT')`
  **bugün de `false`** — grant sütun düzeyinde olduğu için tablo düzeyi sorgu onu
  görmez (ölçüldü: `has_table_privilege` = `f`, `has_any_column_privilege` = `t`).
  Kanıt olamaz. Doğru ölçü `has_any_column_privilege(…,'INSERT') = false`. Tablo düzeyi
  `REVOKE INSERT` sütun grant'larını da siler (ölçüldü, `BEGIN … ROLLBACK`,
  `has_column_privilege(…, 'INSERT')` ile: `slug`, `body`, `published_by` üçü de `f`;
  `has_column_privilege(…, 'body', 'SELECT')` `t` kalır — herkese açık okuma yolu
  etkilenmez).
- `published_by` = `platform_admins.id`; eski satırlar ekranda *"tenant admin
  (legacy)"*. Geri alma = yeni satır (append-only, ADR 0016 §3). ADR 0016 §6'nın
  **256 KiB** gövde sınırı yeni yazma yolunda da uygulanır.
- Tek replika → yayından sonra `legal.Store.Refresh`. ADR 0016'nın HTTP önbellek
  bayatlığı (Sonuçlar ii) değişmez.
- **Tamamen kaldırılanlar:** `TabLegal`, `PanelSection.OperatorOnly`,
  `PanelChrome.Operator`, `mayPublishLegal`, `config.OperatorAdminIDs`,
  `TAPPA_OPERATOR_ADMIN_IDS` (`deploy/k8s/05-config.yaml`, `.env.example`,
  `deploy/README.md`). **K11:** geçişten sonra KF hesabı sıradan bir müşteridir.
  ⚠️ OP-10'un *"`rg` kod+deploy'da 0 (ADR geçmişi hariç)"* kriteri bu hâliyle
  **karşılanamaz**: `db/migrations/00020_create_legal_documents.sql:96-97` iki adı
  (`TAPPA_OPERATOR_ADMIN_IDS`, `mayPublishLegal`) yorumunda taşıyor ve uygulanmış
  migration değiştirilmez (CLAUDE.md §3). Kriter: *"`db/migrations` ve ADR geçmişi
  hariç"*.

### 8. Yapılmayacaklar

- **Müşteri adına giriş (impersonation) YOK** (K7). Gerekirse ileride tenant onaylı,
  süreli, salt-okuma bir destek erişimi — ayrı ADR.
- Kurtarma kodları (K3) · WebAuthn (kapsam dışı; sonraki iş — aşağıda risk 2) ·
  çoklu operatör rolleri · iki kişi kuralı · `admin_users.role`'e üçüncü değer ·
  ayrı süreç/Deployment (K9; OP-19 pilot sonrası yeniden bakar).

### 9. M9-08 kabul kriterlerinin bu tasarımdaki karşılığı

| M9-08 kriteri | Karşılığı |
|---|---|
| *"Operatörün her eylemi `audit_log`'a, ve hangi tenant adına yapıldığı okunabilir."* | **Yeniden okundu (K6):** her eylem `operator_audit_log`'a, satır tenant'ı adlandırır; tenant'ı **değiştirenler** ayrıca o tenant'ın `audit_log`'una aynı tx'te; okumalar yalnız operatör log'unda |
| *"Tenant sınırının aşıldığı her yer, kodda ve ekranda adıyla görünür — sessiz bir çapraz-tenant okuma yok."* | Kodda: yalnız `op_` önekli fonksiyonlar (ADR 0021). Ekranda: tenant-ötesi her ekran girdiği tenant'ı başlıkta **adıyla** gösterir (OP-8). Sessizlik: okuma audit'i veri dönmeden commit edilir (§5) |
| *"İzin listesi boşken panel kimseye açılmıyor, ve bu bir testle sabitlenmiş."* | `platform_admins` boşken kimse giremez; tohumlama ve kurulum sayfası yok (§1, §6) |
| Ayrı rota ağacı; müşteri oturumu oraya hiçbir koşulda giremiyor (kabul 4, özet) | Ayrı host + ayrı çerez + ayrı tablo + ayrı çözümleme (§2, §4) — yapısal |

## Yerine geçilen metinler — madde madde

| Metin | Nerede | Ne oluyor |
|---|---|---|
| **§5 tümü** — *"Kim yazabilir: rol DEĞİL, env izin listesi"*, anahtarın `admin_users.id` olması, reddetme sayfasının çağıranın kendi id'sini basması | [ADR 0016](0016-tenant-kapsamsiz-operator-icerigi.md) §5 | **Yerine geçildi.** Mekanizma OP-10'da kalkar (§7). Genel ilkesi (*"izin listesi anahtarının tekilliği kadar değerlidir"*) §1'in global UNIQUE gerekçesi olarak **taşınır** |
| **§5 "Yeni giriş yok"** maddesi — *"ikinci çerez, ikinci çözümleyici, ikinci kilitlenme hikâyesi. Dört belge bunu haklı çıkarmaz."* | ADR 0016 §5 | **Yerine geçildi** — gerekçe 4 (kapsam farkı) |
| §5 **"Yeni rol yok"** maddesi | ADR 0016 §5 | **Ayakta** — `admin_users.role`'e değer eklenmez (§1) |
| §2'nin *"Kaçınmanın tek yolu operatöre Tappa tenant'ında ikinci bir hesap (ve ikinci bir giriş) vermekti — … orkestratörün 'yeni giriş yok' kararının reddettiği şey"* gerekçesi | ADR 0016 §2 | **Yerine geçildi.** §2'nin **sonucu** (Tappa'nın kendi tenant'ı yok; müşteri oturumlu handler'da `WithTenant(başkası)` yasak) **ayakta** — bu ADR'nin gerekçe 2'si tam olarak odur |
| §2b *"kim yazabilir sorusunun tek cevabı uygulama katmanındaki izin listesidir"* ve *"yazma tarafında DB derinliği yok"* | ADR 0016 §2b | **Yerine geçildi**, OP-10'da kapanır (§7). §2b'nin ölçümü (`tappa_app` yabancı tenant bağlamında INSERT edebiliyor) OP-10'a kadar **doğru** |
| Orkestratörün M7-06 kararı *"yeni giriş yok"* ve M7-06 kartının *"Bu projede süper admin YOK ve icat edilmemeli"* tuzağı | [m7-portal.md](../plan/m7-portal.md) M7-06 → Tuzaklar; ADR 0016 §2/§5'te alıntılı | **Yerine geçildi.** Kartın ikinci tuzağı (*"Tappa'nın kendi tenant'ı §4.5'i ihlal eder"*) **ayakta** |
| *"B — mevcut allow-list modelini genişlet (ayrı rol değil)"* ve *"M9-08 = bu modeli genişletmek"* | `docs/plan/state.md` → M9-08 İŞ 4 paragrafı ve öncesindeki "operatör tasarımı" keşif notu | **Yerine geçildi** (D-B) |
| M9-08 *"Sınırın nasıl kurulacağı"* madde 2 — *"Rota mount edilmiş kalır, handler 403 verir"* | [m9-sonrasi.md](../plan/m9-sonrasi.md) M9-08 | **Yeniden okundu:** 403, *kimliği bilinen ama listede olmayan* bir müşteri içindi; o durum artık yok (müşteri çerezi operatör kimliği değildir). Yeni şekil: yanlış host 404, oturumsuz 303. M6-12'nin dersi (*"sunucu rotayı kendisi reddeder"*) korunur — ret gezinmede değil sunucudadır |
| Sonuçlar — *"izin listesi `.env`'de, veritabanında değil"* | ADR 0016 Sonuçlar | **Yerine geçildi** — operatör `platform_admins`'te (§1) |
| Sonuçlar — *"ürün içinde 'bu metni kim yayımladı' sorusunu cevaplayan bir ekran yok"* | ADR 0016 Sonuçlar | **Yerine geçildi**, OP-10'un sürüm listesiyle (`op_read_*` üzerinden; `tappa_app` hâlâ okuyamaz) |
| **Audit'in yeri** — yayın audit'i **çağıranın kendi tenant'ının** `audit_log`'una | ADR 0016 §2 (*"`audit_log` satırı çağıranın kendi tenant'ına yazılıyor"*) | **Yerine geçildi** — OP-10'dan sonra `operator_audit_log`'a (yasal belge hiçbir tenant'ın değildir; tenant `audit_log`'u yalnız tenant'ı değiştiren eylemler için — K6, §5) |
| M9-08 kabul 1 *"her eylemi `audit_log`'a"* | M9-08 | **Yeniden okundu** (§9, K6) |

**Kod yorumları:** `internal/handler/legaladmin.go` başlığındaki *"THERE IS NO
SUPER-ADMIN IN THIS PRODUCT"* paragrafı ve `internal/config/config.go`
`OperatorAdminIDs` yorumu bu ADR'yle **karar olarak** geçersizdir, **olgu olarak** OP-8
sevk edilene kadar doğrudur; ikisi OP-10'da kodla birlikte kalkar.

**[ADR 0002](0002-tenant-baglami-ve-rls.md) md.6 yerine geçilmedi, yerine getirildi:**
*"Böyle bir ihtiyaç doğarsa ayrı bir rol ve ayrı bir ADR ile gelir. `tappa_app` bu
amaçla asla ayrıcalıklandırılmaz."* — ayrı roller ve ayrı ADR:
[ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md). `tappa_app` hiçbir yeni yetki
almaz.

## Elenen seçenekler

m10-platform.md §3'ün ölçütleriyle:

| Ölçüt | (a) izin listesini genişlet | (b) ayrı süreç / deploy | **(c) hibrit — SEÇİLDİ** |
|---|---|---|---|
| Müşteri oturumu → operatör yüzeyi | aynı çerez, handler başına kapı | yapısal olarak imkânsız | yapısal olarak imkânsız |
| MFA / sunucu tarafı oturum süresi | yok | var | var |
| Tenant-ötesi DB erişimi | `WithTenant(başkası)` ya da bypass | rol + definer | rol + definer |
| Herkese açık kayıttan sömürü | her tenant sahibi potansiyel saldırgan | yok | yok |
| Aynı süreçte RCE | her şey | operatör DSN'ine ulaşılamaz | ulaşılır (K9 — risk 3) |
| Efor | düşük başlar, her ekranla artan risk | en yüksek | orta–yüksek |

- **(a) ELENDİ.** Gerekçelerin dördü de ona çarpar: kabul 4'ü yapıyla karşılayamaz,
  ADR 0016 §2'nin yasakladığı deseni ister, A-0'ın sınıfını büyütür (her yeni ekran o
  tek restoran hesabının gücünü artırır) ve MFA/sunucu tarafı süre getirmez.
- **(b) ŞİMDİ DEĞİL.** Aynı süreçteki RCE'yi operatör DSN'inden ayıran tek şık budur;
  bedeli ikinci bir Deployment, ikinci bir imaj yolu ve ikinci bir ağ yüzeyidir. K9:
  pilot sonrası yeniden bakılır (OP-19); (c)'nin kimlik ve DB tasarımı (b)'ye taşınırken
  değişmez.
- **"Tappa'nın kendi tenant'ı"** (ADR 0016 §2'nin elediği şık) — **hâlâ elenmiş**:
  operatörü bir tenant'a koymak onu `tenants` tablosunda bir müşteri yapar ve
  tenant-ötesi her okuma yine `WithTenant(başkası)` ister.

## Sayılı sınırlar ve kabul edilen riskler

1. **Tek operatör tüm platform gücünü taşır.** Kontrol yalnız audit'tir
   (`operator_audit_log` + değiştirilen tenant'ın `audit_log`'u); iki kişi kuralı ve
   çoklu rol kapsam dışı. Audit, veri döndüren her çağrıyı veri dönmeden **commit**
   eder (§5), ama append-only'si mutlak değildir (superuser — §5).
2. **TOTP gerçek zamanlı aktarma (relay) oltalamasına açıktır.** Bir oltalama sayfası
   parolayı ve o anki kodu anında gerçek host'a aktarırsa oturum açılır; tekrar koruması
   yalnız **ikinci** kullanımı keser. Origin'e bağlı bir ikinci etken (WebAuthn) bunu
   kapatır — sonraki iş. ⚠️ O iş açılırsa bir **CLAUDE.md §4.1 notu** gerekir: platform
   doğrulayıcıları kullanıcı doğrulamasını (UV) cihazda biyometrik yapabilir — biyometrik
   veri sunucuya hiç gelmez, sunucu yalnız bir imza görür — ve `redline-check.sh` R1
   kodda `webauthn` kelimesini FAIL eder (`R1_TRIGGERS`), yani ADR 0012 benzeri bir karar
   ister.
3. **Aynı süreçte RCE iki DSN'i de ele geçirir (K9)** — ve süreçteki her anahtarı:
   session HMAC anahtarı, `TAPPA_OPERATOR_TOKEN_HMAC_KEY`, `TAPPA_OPERATOR_TOTP_KEK`.
   (b) bunu kapatırdı; OP-19.
4. **Veritabanı MFA'yı sürece güvenerek kaydeder.** TOTP doğrulaması süreçte yapılır
   (KEK orada), yani `op_open_session` bir oturumun hak edilip edilmediğini doğrulayamaz
   ve operatörün bağlandığı rol onu çağırabilmek **zorundadır**. O rolün DSN'ini tutan
   biri MFA'lı bir oturum basabilir. ADR 0021'in (i) maddesi uygulama kodunun aktör
   **beyan etmesini** keser, DSN sahibinin oturum basmasını **kesmez** (ADR 0021, sınır
   1). Kestiği şey **izsizliktir**: basılan her oturumun commit edilmiş bir köken satırı
   vardır ve o oturumla veri döndüren her çağrı commit edilmiş bir audit satırı bırakır.
   Yapabildikleri: **başarısız** oturum sondalarını gizlemek ve `op_record_auth_event`
   üzerinden sahte başarısızlık satırları basmak — bu, sayaç yoluyla bir hesabı
   **kilitleyebilir**; TOTP adımını zehirleyip **kalıcı** kilitleyemez, çünkü adım duvar
   saatine bağlıdır (ADR 0021, sınır 3 ve 7). Okuyabildikleri: bütün operatörlerin parola digest'leri (çevrimdışı kırma
   denemesi; bcrypt cost 12, ≥14 rune) ve mühürlü TOTP sırları (KEK olmadan açılamaz) —
   ADR 0021 sınır 11. Kimlik bilgisi yeniden yazımı **kapandı** (enrollment ve kimlik
   bilgisi yazımı definer'da — ADR 0021 sınır 10). Risk 3'ün veritabanı yüzüdür.
5. **Çalınan bir operatör çerezi** 8 saate kadar (boşta 30 dk) geçerlidir; tek çare
   iptaldir. `HttpOnly` + host'a bağlı `__Host-` + `SameSite=Strict` yüzeyi daraltır,
   kapatmaz.
6. **Kurtarma kodu yok.** Operatör cihazını kaybederse geri dönüş `tappa_owner` ile
   `psql` ister (`opadmin reset-mfa`). Kabul (K3): kurtarma kodu, bir kez yazdırılıp
   bir yerde saklanan ikinci bir kimlik bilgisidir.
7. **Canlı kümede rol ve Secret kurulumu kullanıcı işidir** (T44/T45 emsali; ajan
   `tappa-secrets`'a dokunmaz). Yarım kurulum fail-closed'dır: DSN yoksa `/operator`
   503, müşteri paneli etkilenmez.
8. **ADR 0005'in *"Append kuralı"*** yeni kabul edilen risklerin oraya eklenmesini
   ister. Risk 1–4'ün ADR 0005'e eklenmesi OP-4'ün kapsamı dışında bırakıldı (görev
   yalnız iki ADR ve ADR 0016 notu) — **açık iş**; ekleme `cmd/tappa/adr0005_test.go`'nun
   sayım testlerini de günceller.

## Karar verilmedi

**⏳ Sırası gelince kullanıcıya sorulacak (m10-platform.md §6):**
- **OP-K2 / OP-K4 — ops DNS ve IP kısıtının ayrıntısı.** Host adı (`ops.taptime.mt`)
  D-B ile, IP kısıtının **opsiyonel** olduğu K4 ile verildi; DNS kaydı, TLS sertifikası,
  Ingress kuralı, IP kısıtının mekanizması (Ingress mi uygulama mı) ve hangi adresler
  **sorulmadı**. OP-9 ile birlikte (kullanıcı ops adımı).
- **OP-K5 — askıdaki ayın faturalanması** (ADR 0025 / OP-15; bu ADR'nin konusu değil).
- **OP-K12 — T27/T37 plan geçmişi** (OP-17'nin önkoşulu).

**Görev kartında sayıyla/ölçümle karara bağlanacak (bu ADR bilerek sayı koymadı):**
- TOTP kilidinin eşiği N ve kilit penceresi — OP-6/OP-8. (Kilidin yeri karara
  bağlandı: `platform_admins`'teki sayaç, `op_open_session`'ın `UPDATE`'inde sınanır.)
  → **OP-5 notu (2026-09-26):** mekanizma sayısız var olamadığı için 00026 **geçici**
  N = 5, pencere 15 dk koydu; kesin sayılar hâlâ OP-6/OP-8'in (değişiklik = yeni
  migration'da `CREATE OR REPLACE`). Kilidin **şekli** (2. tur, bilinçli ve pinli): sayaç
  yalnız başarıda sıfırlanır — eşikten sonra pencere geçince tek hata yeniden kilitler,
  kilitliyken hata pencereyi uzatır; yalnız `active` hesabın sayacı ilerler. Gerekçe ve
  sayılar ADR 0021 "OP-5 uygulama notu"nda.
- Ara çerezin ömrü ve üç limiter'ın sayıları — OP-6/OP-8 (panel limiter'larının
  aritmetik savunması emsal).
- `operator_audit_log`'un sütun şekli ve `platform_*` tablolarında gönüllü RLS olup
  olmayacağı (ADR 0016 §1 emsali) — OP-5. → **OP-5'te karara bağlandı (2026-09-26):**
  sütunlar `kind` (kapalı küme), `session_id`, `actor_admin_id`, `target_admin_id`,
  `target_tenant_id` (bilerek `tenant_id` değil), `target_scope`, `page_number`,
  `page_size`, `detail`, `at` (`DEFAULT clock_timestamp()`); dört tabloda ENABLE + FORCE
  RLS, tek politika (`tappa_operator` yalnız `active` hesapları okur). Gerekçeler:
  ADR 0021 "OP-5 uygulama notu" ve [m10-platform.md](../plan/m10-platform.md) → OP-5 kart
  düzeltmesi.
- Operatör host'unda **müşteri rotalarının servis edilip edilmeyeceği.** Öneri: iki
  yönlü host kapısı — operatör host'unda yalnız `/operator/*` ve statik dosyalar; çünkü
  operatör host'unda render edilen herhangi bir müşteri sayfasındaki bir XSS, operatör
  çereziyle **aynı origin**'de koşar. OP-8'de ölçülerek.
- Müşteri tarafında `audit_log` okuyan yüzeylerin `actor_kind = "operator"` satırını
  nasıl gösterdiği (`actor_id` `admin_users`'ta bulunmaz) — ilk değiştiren `op_*`'ın
  kartı (OP-15/OP-16).

## Sonuçlar

- **OP-5** (migration + roller + runbook), **OP-6** (`internal/operatorauth`), **OP-7**
  (`OperatorDB` + config), **OP-8** (handler'lar + UI), **OP-9** (`cmd/opadmin`),
  **OP-10** (legal taşıma + izin listesinin emekliliği) bu ADR'yi ve ADR 0021'i
  normatif kaynak alır.
- **Orkestratöre — CLAUDE.md güncellemesi gerekecek** (bu görev CLAUDE.md'ye dokunmaz):
  §3 dizin haritası (`internal/operatorauth`, `cmd/opadmin`, `OperatorDB`); §4.5'in
  *"uygulama `tappa_app` rolüyle bağlanır"* cümlesi (aynı ikili ikinci bir havuzu
  `tappa_operator` olarak açacak); §7'nin *"asla loglanmaz"* listesine **okuma bileti,
  TOTP kodu, enrollment token'ı** (§5). §3'ün *"Kriptografi SADECE burada"* kuralı
  **değişmez**: TOTP zarfı `internal/sun`'da kalır (§1, orkestratör kararı
  2026-09-26).
- **Kullanıcının `tappa-secrets`'a ekleyeceği adlar** (değerler hiçbir dosyaya
  yazılmaz — A-0; ADR 0021 §5'teki listeyle **aynı**): `TAPPA_OPERATOR_DATABASE_URL`,
  `TAPPA_OPERATOR_TOTP_KEK`, `TAPPA_OPERATOR_TOKEN_HMAC_KEY` ve `tappa_operator`
  rolünün parolası.
- 🔴 **OP-10 tuzağı — adı anılan testler.** İzin listesini sabitleyen M7-06 testleri
  ADR 0016'nın gövdesinde, `m7-portal.md`'de, `m10-platform.md`'de ve kod yorumlarında
  **adıyla** anılıyor. OP-10 onları silerse her atıf sarkan atıfa döner ve
  `TestEveryNamedTestExists` kırılır: sarkan atıf envanteri
  (`cmd/tappa/testdata/known-dangling-citations.txt`) bütçeye `!=` ile bağlı ve ADR
  0016'nın gövdesi düzenlenmez. Yol kapalı değil ama pahalı: ya testler yeni davranışı
  ölçecek şekilde **aynı adla** tutulur, ya da envanterin kendi emsali izlenir — 2026-09-19
  girdisi, düzenlenemeyen belgelerde anılan yeniden adlandırılmış bir test için bütçeyi
  **aynı değişiklikte ve gerekçesiyle** bir artırdı (*"the sanctioned exception rather
  than rot"*). Bu ADR o test adlarını bilerek yazmadı.
- Allow-list mekanizması OP-10'a kadar **üretimde yürürlüktedir**; bu ADR davranışı
  değiştirmez, kararı değiştirir.
