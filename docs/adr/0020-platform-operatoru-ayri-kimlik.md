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
  **OP-6 notu (2026-09-30, doğrulama 6. tur), ölçümle:** anahtarları TUTAN iki dışa açık tip
  de kendini redakte eder — `*Authenticator` (`operatorauth.Authenticator(redacted)`) ve
  `Config` (`operatorauth.Config(redacted)`), token tiplerinin beş yöntemiyle. Önce: `New`'un
  döndürdüğü `*Authenticator`'ın `%v`/`%+v`'si ve bir `Config`'in `%+v`'si TOTP KEK'ini ve
  token HMAC anahtarını ondalık bayt listesi olarak basıyordu, slog'un text handler'ı da;
  JSON basmıyordu, ama yalnız `Config`'in func alanı `Marshal`'ı düşürdüğü için (5. denetçi).
  ~~`Authenticator`'ın anahtarları ayrıca bir işaretçinin arkasındadır: … adres görünür, bayt
  değil. **Açık kalan tek yol, adıyla:** … `Config` DEĞERİ …~~ — **7. turda yanlışlandı** (6.
  denetçi, ölçümle): (1) fmt'nin `badVerb`'ü, işaretçinin kabul etmediği bir fiilde
  (`%s %q %e %f %t %c %U`) işaretçiyi derinlik 0'da BİR KEZ açar; bir dizi, dilim, struct ya
  da map'i gösteren işaretçiyi açar — `*authKeys` dört anahtarı, `Secret`'in ve
  `db.SealedSecret`'in `*[]byte`'ı düz TOTP sırrını ve zarfı bastı; (2) `Config`'in açık yolu
  "tek" değildi: `%p` ve `%w` (`Sprintf` ve `Errorf`) fmt'nin `erroring` bayrağıyla
  yöntemleri devre dışı bırakır ve yansımayla basar, dışa kapalı alan yolu da her fiilde
  sızıyordu.
  **OP-6 notu (2026-09-30, doğrulama 7. tur) — kural ve ölçülen matris:** bir sır ya da
  anahtar baytı dışa açık ya da redakte eden bir tipte YALNIZ tek alanlı bir struct'ın
  içinde, `*string`'in arkasında durur (repodaki öteki redakte eden tiplerin emsali;
  `*string` `badVerb`'ün açmadığı bir işaretçidir). `Secret`, `db.SealedSecret` `*string`'e
  döndü; `Config`'in iki anahtarı ve `Authenticator`'ın dört anahtar alanı yeni, kendini
  redakte eden `operatorauth.Key` (`struct{ v *string }`, `NewKey([]byte)`, dışa açık
  erişimci YOK). *8. tur, orkestratörün kararı (çelişki giderildi):* `internal/db`
  `internal/config`'i import ettiği için `config.Config` bir `Key` tutamaz; operatör anahtarları
  orada öteki anahtarlar gibi ham `[]byte` durur ve anahtar ayrılığı reddi orada, ham
  değerlerde koşar (`keySeparation` emsali; `config.Config`'in kendi redaksiyonu repo genelinde
  ayrı bir iş); `Key`'e dönüşüm `cmd/tappa`'nın wiring'inde `NewKey` ile yapılır;
  "redaksiyonsuz yapıya kopyalanmaz" kuralı `operatorauth` tarafı içindir. Hash/HMAC/`Seal`/
  bcrypt'in istediği `[]byte` yalnız kullanım anında, bir kopya olarak üretilir (TOTP sırrının
  kopyası kullanımdan sonra silinir; anahtarınki silinmez — anahtar süreç boyunca bellektedir). **Ölçülen matris** (`TestLeak_NoSecretOnAnyPrintingPath`,
  sızıntı testinin `render`'ı): kapalı tip kümesinin her specimen'i (13 tip, `Key` dahil) ×
  fmt'nin 22 fiili (`%p` ve `%w` dahil) × 6 biçim (değerin kendisi, dışa açık ve dışa kapalı
  `any` alanı, dışa kapalı alanda işaret edilen değer, dilim elemanı, map değeri) × `Sprintf`
  ve `Errorf` + slog text/JSON + `json.Marshal` — **0 sızıntı**; yöntemlere ulaşılan her
  yolda yer tutucu basılır. **Sayılı sınırlar — tek liste:** [m10-platform.md](../plan/m10-platform.md)
  → OP-6 kart düzeltmesi, md. 18, *Tek liste (11. tur)* — ürün, kapalı küme, alan kuralı,
  specimen araması ve sızıntı testi sınırları orada numaralı; burada ne tekrarlanır ne
  aralığı yazılır, çünkü 10. turda kopyalar ayrıştı ve 12. turda buradaki aralık bayatladı. *(8. tur; 9. turda ölçüme göre yeniden yazıldı — bir KURAL olarak burada kalır:)*
  `store`'un DÜZ bir alanı — OP-7'nin tipi — `Authenticator`'ın değer ya da
  işaretçi olarak tutulmasından bağımsız olarak basılabilir (8. denetçinin 28 fiillik
  ölçümü: düz alanlı değer store'u her fiilde, düz alanlı işaretçi store'u ve dışa kapalı
  alandaki `*Authenticator` 14 fiilde; `*string` alanlı store hiçbirinde); kural OP-7
  devrinde: düz alan yok. ~~… ve bütçe haritalarının anahtarlarını (istemci adresleri,
  operatör id'leri) bir kez açıp basar …~~ → **8b, kapandı:** `Authenticator.limits` artık
  `*limits` (pinli: sızıntı matrisinin `Authenticator` specimen'inin bütçe haritası dolu,
  `limits limits` geri dönüşü kırmızı; 8. denetçi 0 isabet ölçtü). Kapalı küme — 9. turdan
  beri KESİN tiplerle (derleyicinin export verisi); 10. turdan beri yürüyüş go/types'ın tip
  grafiğini tükenmiş bir anahtarla tam dolaşır (tanınmayan tür = kırmızı; her paketin tipi,
  her struct alanı); 11. turdan beri KAYIT modülün her paketinin adlı tipleridir (`sun.EV2Auth`
  dersi), alan yürüyüşü her paketin yapısına girer, alan girdileri tipiyle pinlidir ve
  specimen'in aradığı sırlar tuttuğu redakte değerlere karşı denetlenir
  (`TestSpecimens_SearchEveryRedactedValueTheyHold`); 12. turdan beri kural (1) istisnasızdır
  (her alan — fonksiyon ve `okFieldTypes` tipliler dahil — adıyla ve tipiyle), kural (2)
  fmt'nin ve `encoding/json`'un ölçülen işaretçi davranışını okur
  (`TestExportedTypes_ExemptFormsPrintNoKeyBytes`) ve gerekçe metinleri gözden geçirenin
  iddiasıdır (tek liste S11); ~~kalan sınır yalnız çalışma zamanında
  doldurulan `any` ve reflection~~ *(11. tur: sınırlar yukarıdaki tek listede)* — ve pinler:
  `TestExportedTypes_EveryOneIsASpecimenOrANamedException`,
  `TestExportedTypes_CarryNoPlainStringField` (alan ve tür kuralları).
  **OP-6 notu (2026-10-01, 12c — güvenlik denetimi kapanışı):** (i) bekleyen enrollment
  blob'u artık KEK'in KENDİSİYLE değil, ondan türetilmiş bir alt anahtarla mühürlenir —
  `HMAC-SHA256(KEK, "taptime/operator/enrollment-pending/v1/key-derivation")`, challenge
  anahtarının türetme emsali: `BeginEnrollment` kimliksiz erişilebilir ve saklanan zarfların
  anahtarı için bir şifreleme kehaneti olmamalı; saklanan zarf (§1) ham KEK'le kalır. (ii)
  §3'ün bütçelerine adres başına bir enrollment payı eklendi: `enrollAddr` 3/10 dk, süreç
  geneli `enroll`'dan (10/10 dk) önce. *(12d düzeltmesi, ölçüldü:)* pay anahtarın KENDİ
  penceresi başınadır ve pencereler hizalı değildir — pencere sınırında tek adres bir süreç
  penceresine 5'e kadar koyar, tek bir süreç penceresi iki anahtar ve önceki bir istekle
  tükenir; süreç bütçesini SÜREKLİ tüketmek en az dört hız anahtarı ister (dağıtık saldırgan
  sınırı ve çaresi OP-8'in). Sayılar OP-8'in
  aritmetiğiyle değişebilir. Ayrıntı: m10-platform.md, OP-6 md. 8 ve 12. tur bloğunun 12c
  satırı.
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
  **OP-6 düzeltmesi (2026-09-26), ölçümle:** *"oturum öncesi satırlar … süreç genelinde bir
  tavanla"* cümlesi **parolasız** türler (`unknown_email`, `login_failed`,
  `enrollment_failed`) için harfiyen uygulandı (30 satır / 10 dk, tek anahtar). `totp_failed`
  ve `locked` o ortak tavanın **dışındadır**: kilit sayacı yalnız `totp_failed` satırıyla
  ilerler, yani parolasız çöp ortak tavanı doldurup `totp_failed`'ı susturabilseydi kilidi
  **kapatırdı** — ölçüldü (2026-09-30'da yeniden üretildi): o yönlendirmeyle, tavan
  tükenmişken 5 yanlış kod **0** satır ve sayaç **0** bıraktı (doğrusu 5 ve 5). Bu iki tür
  yalnız doğru parolayla basılmış bir giriş challenge'ını tutanın harcayabildiği, kod
  denetlenmeden ÖNCE harcanan hesap bütçesiyle (10 / 10 dk / operatör) × aktif operatör
  sayısıyla sınırlıdır — IP'den bağımsız, ama tek bir sayı değil. Kural TOTP adımının iki
  kolu için de geçerlidir: veritabanının reddettiği tekrar edilen kodun `totp_failed`'ı ve
  kilitliyken doğru kodun `locked`'ı da tavanın dışındadır (aynı test, 2026-09-30).
  Ayrıntı ve sayılar: [m10-platform.md](../plan/m10-platform.md) → OP-6 kart düzeltmesi,
  md. 8–9.
  **OP-14 düzeltmesi (2026-10-06, A fazı — migration 00031):** *"yalnız başarısızlık"*
  artık doğru değil — kapalı kümenin altıncı türü `password_ok`: **doğru parolası kabul
  edilmiş, ikinci faktörü henüz tamamlanmamış** bir girişin kalıcı izi (OP-6'nın 12c devri:
  bugüne dek yalnız bir süreç log satırıydı). Oturum öncesidir (aktör ve oturum yok), hesabı
  yalnız **id** ile adlandırır, kilit sayacına **dokunmaz**; bir oturumun doğuşu hâlâ yalnız
  köken satırıdır (`login`, `enrollment`). Yazıcısı ve **hesap başına** tavanı OP-14 C
  fazınındır — parolasız ortak tavana konmaz (kart taslağının C1'i: parolasız çöp onu
  susturamamalı). Ayrıntı: [ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md) → §1
  (düzeltilen tablo ve madde), sınır 7, "OP-14 uygulama notu".
  **OP-14 C notu (2026-10-07) — 12c'nin süreç log satırı kalıcı satıra döndü:** parola adımı,
  karşılaştırma başarısından **sonra** ve challenge (ara çerezin değeri) basılmadan **önce**
  `password_ok` satırını yazar; 12c'nin Info satırı (`operator first factor verified; second
  factor pending` + `operator_id`) **kaldırıldı** — iz artık satırın kendisidir ve tavanların
  altında `operatorauth` girişte hiçbir log satırı yazmaz (ölçüldü). Tavan **operatör başına** 10 / 10 dk,
  IP'den bağımsız, parolasız ortak tavana **konmaz**; aşılınca istek yine hizmet görür, satır
  yazılmaz, pencere başına operatör başına **tek** WARN (tür, operatörün id'si, sayılar —
  istekten hiçbir şey). Satır yazılamazsa parola adımı **hata** döner (fail-closed): challenge
  yok, çerez yok (yüzeyde 503). *(2. tur, üçüncü gözün B1'i, ölçüldü: ilk turda satır isteğin
  bağlamıyla yazılıyordu ve karşılaştırma sırasında bağlantısını kapatan istemci tavanı satırsız
  harcıyordu — on kesik istek pencerenin izini boşalttı. Artık satır isteğin iptalinden **ayrık**
  yazılır, kendi süresiyle (`FirstFactorRecordGrace` 5 sn, HTTP drenajının içinde), ve başarısız
  yazım şarjını **iade eder**.)* *(3. tur, güvenlik denetiminin F1'i, ölçüldü: tavanı uçuştaki
  ve sonra düşen yazımlar tutarken gelen doğru parola satırsız challenge alıyordu. Artık tavanın
  üstünde satırsız hizmet yalnız penceresinde o operatörün **yazılmış** 10 satırı varken verilir;
  aksi hâlde adım fail-closed reddedilir (503) ve her fail-closed ret operatörü id'siyle pencere
  başına tek WARN satırında adlandırır.)* Ayrıntı ve ölçümler: ADR 0021 → "OP-14 C fazı eki"
  md. 8–9.
  **OP-14 E notu (2026-10-07) — girişin geri kalan satırları da istemciden ayrık:** parolasız
  satırlar (`login_failed`, `unknown_email`, `enrollment_failed`), kod adımının hesap bütçesinden
  sonraki her ifadesi (arama, `totp_failed`, `op_open_session`, `locked`) ve enrollment'ın
  `op_complete_enrollment`'ı isteğin iptalinden **ayrık**, ifade başına kendi süresiyle
  (`SignInStatementGrace` 5 sn; en uzun yol üç ifade, 15 sn — HTTP drenajının içinde) koşar; ret
  sayfaları da iptalden ayrık render edilir. Ölçülen açık (OP-14 C güvenlik denetiminin P4 ve P8'i):
  yarı kapalı bağlantıda yanlış parola satırsız 500, doğru parola 303 alıyordu (izsiz parola tahmini);
  kesilen kod denemeleri satırsız, sayaçsız hesap bütçesini tüketiyordu (izsiz kilitlenme). Artık
  yarı kapalı her ret açık kalanın aldığı durum ve baytları alır ve satırını bırakır; N kesilen kod
  denemesi N sayılan denemedir — kilitliyken kesilen doğru kod da `locked` yazar ve yanlış kodun
  cevabını alır. Yazılamayan satır adımın hatasıdır (503), ret ya da başarı değil; parolasız ortak
  tavan değişmedi. Parola adımının araması istemciyi izlemeye devam eder: aramadan önce giden istemci
  hiçbir şeyle karşılaştırılmaz. Ayrıntı, ölçümler ve sınırlar (LE1–LE11; 2. tur md. 10): ADR 0021 →
  "OP-14 E uygulama notu".
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
- **QR notu (2026-10-09) — karar değişti: enrollment ekranı anahtarı QR kodu olarak da
  gösterir.** Yukarıdaki *"QR kütüphanesi yok: enrollment ekranı base32 sırrı ve
  `otpauth://` URI'sini metin olarak gösterir"* cümlesinin ilk yarısı geçerlidir —
  kütüphane yok —, ikinci yarısının yerini bu not alır. **Kullanıcı kararı (2026-10-09):**
  canlıdaki ilk operatör kaydında Authenticator uygulaması için QR beklendi. CLAUDE.md §1
  değişmedi: kodlayıcı **depoda, saf Go, yalnız standart kütüphane**; `go.mod`, `go.sum`
  diff'i boş.
  - **Kodlayıcı — `internal/qrcode`.** Byte modu; hata düzeltme L/M/Q/H (sayfa **M**
    kullanır); sürüm 1–40, veriyi tutan en küçüğü kendiliğinden; Reed–Solomon (GF(2^8),
    0x11D); 8 maskeden §7.8.3'ün ceza puanı en düşük olanı (eşitlikte küçük numara); biçim
    bilgisi ve sürüm ≥ 7'de sürüm bilgisi; `Bitmap(QuietZone)` 4 modüllük sessiz alanı
    ekler. Sığmayan girdi `ErrTooLong`'u sarar; hata metni yalnız uzunluk ve seviye
    adlandırır, girdiyi değil. Paket durum tutmaz, log yazmaz.
  - **Doğruluk — referans libqrencode 4.1.1** (`qrencode -8 -m 0 -t ASCII`;
    `internal/qrcode/testdata/generate.sh` üretir, testler onu **çağırmaz** — CI'da yok —,
    çıktısı commit edilir): altı adlı vektör tam matris olarak (kısa ASCII L/M/Q/H → sürüm 1;
    112 bayt M → sürüm 7, sürüm bilgisi blokları; sahte anahtarlı bir `otpauth` URI'si M →
    sürüm 8) ve 160 satırlık tarama sha256 olarak (her sürüm × her seviye; girdi, o sürümün
    referansın `--strict-version`'ının kabul edip bir bayt fazlasını reddettiği kapasitesi
    kadar). Referansın seçtiği maskeye zorlanan matris **166/166** eşit. Kodlayıcının **kendi**
    maske seçimi 139/166'da referansınkiyle aynı; kalan 27 iki kodlayıcının ceza kurallarını
    **okumasından** gelir — referans 1:1:3:1:1 desenini en çok bir kez sayar (bu paket ışık
    tarafı başına), koyu payını tam yüzdeye yuvarlayıp bantlar (bu paket tam payı) —; referansın
    okumasının test içindeki bir modeli 166/166 seçimi yeniden üretir. Kodlayıcıyla kod ve
    tablo paylaşmayan bir çözücü (`read_test.go`: Tablo 9 ve E.1 elle, log/antilog GF, sendrom,
    BCH bölmesi) önce referansın matrislerini girdilerine okur, sonra Encode'un sürüm 1–10 ×
    4 seviye çıktısını. **Bir kez ölçüldü, test değil:** Apple Core Image'ın QR dedektörü
    Encode'un 480 PNG'sinin (160 kendiliğinden maske + sürüm 1–10 × 4 seviye × 8 maske)
    480'ini girdisine, Chrome'da render edilen sayfanın ekran görüntüsünü sayfadaki URI'ye
    çözdü.
  - **Çizim — satır içi SVG, `enrollPage`'in kendi yanıtında.** Tek `<path>` (her koyu modül
    için bir birim kare alt yolu; modül başına `<rect>` değil) ve zemin için tek `<rect>`;
    `viewBox` modül biriminde, `shape-rendering="crispEdges"`; renkler SVG `fill`
    öznitelikleri — modüller ink `#152219`, zemin paper `#FFFDF4` (16,17:1; koyu modül açık
    zemin, tersi değil); 4 modüllük sessiz alan çizimin içinde, çerçeve (1 px `line`
    outline) dışında; erişilebilir ad `role="img"` + `aria-label` + `<title>` *"Scan with
    your authenticator app"*. Metin anahtar — base32 ve URI — QR'ın **altında** kalır (QR
    okumayan uygulama için). Boyut modül başına tam piksel: 14 baytlık `ops.taptime.mt` ile
    sürüm 8, sessiz alanla 57 modül, **228 × 228 px**; Chrome'da 390, 360 ve 320 px'lik
    düzende 228, 280 px'lik düzende sütuna sığmak için 198 (bir kez ölçüldü). URI 134 bayt
    + host'tur: 18 bayta kadar host sürüm 8, 46 bayta kadar sürüm 9.
  - **CSP değişmedi:** `enrollCSP` bayt bayt aynı; `style=`, `<style>`, `data:` URI'si,
    `<img>`, harici görsel yok. Chrome'da ölçülen tek politika ihlali betiğindir (sayfa
    ölçüm için `127.0.0.1`'den servis edildi, politika `ops.taptime.mt`'yi adlandırır) —
    SVG için ihlal yok.
  - **Güvenlik iddiası (üç parçalı).**
    - **TEHDİT MODELİ:** QR kodu, sayfanın zaten taşıdığı TOTP sırrının başka bir
      biçimidir; tehdit, bu biçimin sırrı sayfanın kendi gövdesinden başka bir yere —
      başka bir yanıta, bir URL'ye (yol ya da sorgu: ingress log'u, tarayıcı geçmişi,
      Referer), bir önbelleğe, bir log satırına — taşıması ya da sayfadaki anahtardan başka
      bir şeyi kodlamasıdır; sayfanın kendisi tasarlanmış çıkıştır (leak testinin D3'ü).
    - **PART I — ölçülen davranış, adıyla test:**
      `TestEnrollQR_IsTheKeysURIInkOnPaper` — ilk yüklemede tek `svg`, öznitelikleri tam
      olarak yedi ve bu sırada (`style`, `xmlns`, `href` yok), çocukları `title` + `rect` +
      `path`; zemin ve modül renkleri `tailwind.config.js`'in paper ve ink'i; yol yalnız
      birim karelerden oluşur ve koyu kümesi sayfanın yazdığı URI'nin `qrcode.Encode(M)
      .Bitmap(4)`'üne eşittir, sessiz alanda koyu modül 0; o URI sayfanın yazdığı base32
      anahtarı taşır; sıra QR → anahtar → URI; gövdede ` style=`, `<style`, `data:`,
      `<img`, `xlink:`, `<use`, `<image`, `<foreignObject` yok; CSP tek değer ve
      enrollment politikası, `Cache-Control: no-store`, `Referrer-Policy: no-referrer`;
      ≥ 200 px, modül başına tam piksel ·
      `TestEnrollQR_OnlyOnTheFirstLoad` — parolaları farklı yeniden render'da `svg` ve
      anahtar yok; `screens()`'in 40 render'ından yalnız QR matrisli enrollment render'ı
      `svg` taşır ·
      `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` — yeni grup **G19**
      (sayfanın SVG yolu; never-log maddesi N4'e bağlı) on biçimde (R1–R10), dört yüzeyde
      (S1–S4), 99 kolda yalnız kendi sayfasının gövdesinde (D3: A11 ve yeni **A99**); A99
      aynı linkin ikinci açılışıdır — yeni anahtar, yeni QR, A11'inki hiçbir yüzeyinde yok ·
      `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` — enrollment sınıflarının
      politikası değişmedi · `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass` — yeni rota
      yok (yöntem × rota çiftleri aynı yirmi) · `TestEncode_TheRefusalCarriesNoInput` — iki
      hata metni girdinin dört baytlık hiçbir dizisini taşımaz. Kodlayıcının doğruluğu:
      `TestEncode_MatchesTheReferenceVectors`, `TestEncode_MatchesTheReferenceSweep`,
      `TestEncode_TheMaskChoiceDiffersFromTheReferenceOnlyInThePenaltyReading`,
      `TestEncode_ChoosesTheLowestPenaltyMask`, `TestPenalty_ScoresEachRuleByTheStandard`,
      `TestEncode_OneByteMoreTakesTheNextVersion`, `TestAlignmentPositions_AreTableE1`,
      `TestFormatAndVersionInfo_AreTheStandardsCodewords`, `TestBitmap_AddsTheQuietZone`,
      `TestRead_ReadsTheReferenceMatrices`, `TestRead_ReadsWhatEncodeWrote`,
      `TestRead_RefusesABrokenSymbol`.
    - **PART II — adı konmuş pinler (yapıcının koşturduğu 22 mutasyon, her biri tek
      düzenleme, geri alındı; kırmızıya dönenler):** maske 4'ün formülü
      `(x/2+y/3)` → `TestEncode_MatchesTheReferenceSweep`, `…Vectors`,
      `…TheMaskChoiceDiffers…`, `TestRead_ReadsWhatEncodeWrote` · GF polinomu `0x11B` → aynı
      dört + `TestRead_RefusesABrokenSymbol` · biçim maskesi `0x5413` ve sürüm üreteci
      `0x1F27` → her biri aynı beş + `TestFormatAndVersionInfo_AreTheStandardsCodewords` ·
      sürüm 10'da 8 bitlik sayı → `…Sweep`, `…TheMaskChoiceDiffers…`, `…ReadsWhatEncodeWrote`
      · ECC kod sözcükleri ters sırada, ikinci biçim kopyası ters bitlerle → her biri
      `…Sweep`, `…Vectors`, `…TheMaskChoiceDiffers…`, `…ReadsWhatEncodeWrote`,
      `…RefusesABrokenSymbol` · hizalama adımı formülü → bu beş + `TestAlignmentPositions_AreTableE1`
      · N3 ağırlığı 0 → `TestPenalty_ScoresEachRuleByTheStandard`, `…TheMaskChoiceDiffers…` ·
      N4 0 → `TestPenalty_ScoresEachRuleByTheStandard` (maske sayısı 139'da kaldı) · sessiz
      alan kaldırıldı (`Bitmap(0)`), açık modüller çizildi, `svg`'ye `style=`, `aria-label`
      kaldırıldı, QR başka bir dizginin (URI + `#`), anahtar QR'ın üstünde → her biri
      `TestEnrollQR_IsTheKeysURIInkOnPaper` · renkler ters (ink ↔ paper) → o test +
      `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` · QR ayrı bir uç noktada
      (`GET /operator/enroll/qr` bağlandı) → `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass`
      · QR bir URL'den (`<image href="/operator/enroll/qr?d=…">`) →
      `TestEnrollQR_IsTheKeysURIInkOnPaper`, leak testi,
      `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` · `no-store` → `private` →
      `TestEnrollQR_IsTheKeysURIInkOnPaper` ve sekiz başlık testi · QR sayfalar arasında
      önbellekte (ikinci açılış ilkinin QR'ını çizer) ve URI'nin bir Debug log satırında →
      her biri leak testi.
    - **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.
  - **Sınırlar.** (1) QR matrisi ve SVG yolu silinmez — render edilen dizgiler gibi (OP-6
    md. 18 P6). (2) Kodlayıcının reddi bir istekle ulaşılamaz: host en çok 253 bayt, URI en
    çok 387, sürüm 40-M 2331 bayt tutar; ulaşılsaydı sayfa anahtarı yine metin olarak
    gösterir ve ERROR satırı yalnız uzunluk ve seviye taşır (`TestEncode_TheRefusalCarriesNoInput`
    hata metnini ölçer; dalın kendisi bir testle sürülmez). (3) QR, ekrandaki sırrı bir
    kameranın uzaktan yakalamasını 32 karakterlik metinden kolaylaştırır; aynı sır aynı
    ekranda zaten metin olarak duruyordu — hafifletenler tek kullanımlık, 30 dakikalık link
    ve anahtarsız yeniden render; kullanıcı kararıyla kabul edildi. (4) Maske seçimi
    referansınkinden 27/166 durumda farklıdır; ikisi de geçerli sembol, ikisi de standardın
    sözünün bir okuması.

### 4. Rota ve host

- **Rotalar:** `/operator/login`, `/operator/login/totp`, `/operator/enroll`,
  `/operator/logout`, `/operator` (konsol: menü — okuma yapmaz; oturum bütçesinden bir
  birim, audit satırı yok), `/operator/tenants` (tenant listesi: `GET` her tenant'ın ilk
  sayfası, `POST` arama ve her sayfa — OP-11), `/operator/tenants/{id}` (bir tenant'ın genel
  bakışı), `/operator/tenants/{id}/plaques` (bir tenant'ın plaket envanteri — OP-13),
  `/operator/legal`, `/operator/billing`, `/operator/audit` (operatörün kendi audit günlüğü:
  `GET` her türün ilk sayfası, `POST` tür filtresi ve her sayfa — OP-14).
  *(OP-11 notu, 2026-10-03: bu madde ilk yazıldığında `/operator`'u tenant listesi
  sayıyordu. Liste bir okumadır — iki bütçe birimi ve bir `read` satırı — ve her girişin
  indiği sayfa bu bedeli taşımasın diye kendi rotasına alındı; konsol ona link verir.
  Gerekçe ve ölçüm: ADR 0021 → "OP-11 B fazı eki" md. 1.)*
  *(OP-13 notu, 2026-10-06, bütçe: yukarıdaki OP-11 notunun *"iki bütçe birimi"* o günün
  kaydıdır. OP-13 B'den itibaren bir okuma `sessionGate`'te oturum bütçesine (`sessionLimit`
  100) bir birim ve ayrı bir `readLimit`'e (60 / 10 dk) handler'da bir birim sayılır. Bkz. ADR
  0021 → "OP-13 B fazı eki" md. 8.)*
  *(OP-13 notu, 2026-10-06: bu madde ilk yazıldığında plaket ekranını `/operator/plaques`
  diye adlandırıyordu. Envanter BİR tenant'ındır — okuması tenant'ı adlandırır, başlığı onun
  adını taşır — bu yüzden tenant'ın altına alındı: `GET /operator/tenants/{id}/plaques`,
  genel bakış ona link verir, konsol vermez; `/operator/plaques` kayıtlı değildir
  (`TestSurface_TheScreensOfLaterTasksAreNotMounted` onu hâlâ sürer). Platform geneli bir
  stok görünümü bu görevde yok (OP-13 kararı K13-3). Gerekçe ve ölçüm: ADR 0021 →
  "OP-13 B fazı eki" md. 1.)*
  *(OP-14 notu, 2026-10-07: `/operator/audit` monte edildi — konsolun grubunda, aynı zincir;
  konsol ona link verir. `GET` URL'den hiçbir şey okumaz (her türün ilk sayfası); tür ve sayfa
  `POST`'un gövdesindedir (tenant aramasının tek biçim kuralı; tür kapalı bir kümeden bir
  değerdir, kişisel veri değildir, kural yine de tek biçimde tutulur). Her görüntüleme bir
  okumadır: bir `read` satırı, bir okuma birimi. Alt yolları (`/operator/audit/…`) ve bir
  tenant'a göre audit görünümü (`/operator/tenants/{id}/audit`, orkestratör kararı K14-5: v1'de
  yok) kayıtlı değildir ve yönlendiricinin kendi 404'ünü verir
  (`TestSurface_TheScreensOfLaterTasksAreNotMounted`). Gerekçe ve ölçüm: ADR 0021 → "OP-14 B
  fazı eki".)*
  *(OP-12 notu, 2026-10-07: faturalama ekranı BİR tenant'ındır — okuması tenant'ı adlandırır,
  başlığı onun adını taşır — bu yüzden tenant'ın altına alındı (orkestratör kararı K12-1 (a)):
  `GET /operator/tenants/{id}/billing` ilk sayfadır ve URL'den yoldaki id dışında hiçbir şey
  okumaz; `POST /operator/tenants/{id}/billing` gövdede `page` taşır (1..5, her biri tenant'ın
  kendi on iki ayı); konsolun grubunda, aynı zincir; genel bakış ona link verir, konsol vermez.
  Yukarıdaki listedeki platform geneli `/operator/billing` (bir ayın her tenant'ı: bir *fatura iş
  listesi*) kayıtlı değildir — K12-1 (b), OP-K12'nin cevabından sonra ayrı bir kart — ve
  yönlendiricinin kendi 404'ünü verir; ekranın alt yolları (`…/billing/`, `…/billing/<ay>`,
  `…/billing.csv` — K12-4: CSV yok) da öyle (`TestSurface_TheScreensOfLaterTasksAreNotMounted`).
  Her görüntüleme bir okumadır: bir `read` satırı, bir okuma birimi; okumanın ikinci aşaması
  kendi süre sınırıyla (15 sn) koşar, aşınca 503. Gerekçe ve ölçüm: ADR 0021 → "OP-12 B fazı
  eki".)*
  *(OP-16 notu, 2026-10-07: bir tenant'ın VAT numarası ve VIES'e yeniden sorulması da BİR tenant'ındır
  — okuması tenant'ı adlandırır, başlığı onun adını taşır — bu yüzden tenant'ın altındadır: `GET
  /operator/tenants/{id}/vat` ekrandır ve URL'den yoldaki id dışında hiçbir şey okumaz; `POST
  /operator/tenants/{id}/vat` VIES'e sorar ve **gövdeden hiçbir şey okumaz** — VIES'e ve yazmaya giden
  numara isteğin kendi audit'li okumasınındır (orkestratörün K16-1'i); konsolun grubunda, aynı zincir;
  genel bakış ona link verir, konsol vermez. Cevap kaydedilirse `303` aynı ekrana (POST → 303 → GET —
  yüzeyin sabit olmayan tek `Location`'ı: tenant'ın kendi yolu, yoldaki id'den); VIES cevap vermezse
  yazma yok, `503` ve aynı ekran bir cümleyle; numara VIES'in alacağı biçimde değilse `422`, VIES'e
  istek yok. Bütçe: `GET` bir okuma; `POST` bir okuma ve — VIES'e gerçekten sorulacaksa — oturumun
  VIES bütçesinden bir birim (10 / 10 dk, `viesLimit`); `303`'ün `GET`'i bir okuma daha. Okuma (iki
  yöntemde de) kendi süre sınırıyla (5 sn) koşar; `POST`'un okuması, VIES çağrısı (4 sn) ve yazması
  (10 sn) istemciden ve yönlendiricinin süresinden ayrıktır, toplamları (19 sn) HTTP boşaltma süresinin
  (20 sn) içindedir *(2. tur)*. Ekranın alt
  yolları (`…/vat/`, `…/vat/check`) ve platform geneli bir `/operator/vat` kayıtlı değildir ve
  yönlendiricinin kendi 404'ünü verir (`TestSurface_TheScreensOfLaterTasksAreNotMounted`). Gerekçe ve
  ölçüm: ADR 0021 → "OP-16 B eki".)*
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
- **OP-7 notu (2026-10-01), ölçülerek** (ayrıntı: [m10-platform.md](../plan/m10-platform.md)
  → OP-7 kart düzeltmesi): (i) yüzeyin **üç** hâli var: yapılandırma yok → 503 *"not
  configured"*; yapılandırma tam ama operatör veritabanına **ulaşılamadı** — *(2. tur:
  KAPALI bir liste: ağ, ad çözümü, zaman aşımı, iptal edilmiş açılış ve SQLSTATE sınıfı 08 ve
  28, `3D000`, `53300`, `57P01`–`57P03`; sunucunun öteki her cevabı — ör. `42501`, `22023`,
  `42704` — ulaşılan şeyin reddidir ve açılışı durdurur; *2b:* bir TLS hatası ya da tanınmayan
  bir hata da ret'tir, ve birden çok denemeli bir bağlantı — çok host'lu DSN, `sslmode=prefer`
  — ancak **her** denemesi ulaşılamazsa ulaşılamazdır)* → yine 503, *"unavailable"*, ERROR
  satırı, **süreç açılır ve müşteri
  ürünü servis verir**; ulaşılan şeyin reddi (rol kapısı, log parametresi geri okuması), bozuk
  DSN ya da yanlış boyda anahtar → **açılış reddi**. Gerekçe risk 7'nin ruhu ve bir geri
  yükleme yolu (B YOLU): rol parolaları bir veritabanı dökümünde yoktur, taze kümede
  `01-roles.sql` `tappa_operator`'ı yeniden NOLOGIN yaratır, dolu bir DSN 28P01 alır —
  ölümcül bir kuralla bu bir müşteri kesintisi olurdu (28P01'in ulaşılamaz sayıldığı yanlış
  bir parolayla ölçüldü; geri yüklemenin kendisi koşulmadı). (ii) `TAPPA_OPERATOR_HOST` tek yazımlıdır (küçük harfli DNS adı; şema, port, yol
  yok) ve `TAPPA_BASE_URL`'in host'u **olamaz** (`config.Load` reddeder). *(2. tur: kural
  yalnız bu host'u bilir; ingress'in öteki müşteri host'ları — `www.taptime.mt`,
  `tappa.everva.com.tr` — kabul edilir. Operatör yüzeyini her müşteri host'unun dışında tutmak
  OP-8'in iki yönlü host kapısının işidir.)* Host kapısı ve yanlış host 404 hâlâ OP-8'in;
  OP-7'de yapılandırılmış yüzey rota sunmaz (404).
- **OP-8 notu (2026-10-02; 4. turda bu biçimde yeniden yazıldı, 5. turda (iv), (v), (viii) ve (III) düzeltildi, 6. turda (v), (viii), (III) ölçülen biçim ve sınıf kümesine daraltıldı ve sınırlar eklendi)** (kararların gerekçesi,
  sayılı sınırlar ve devirler: [m10-platform.md](../plan/m10-platform.md) → OP-8 kart
  düzeltmesi). Üç parça (OP-7 kart düzeltmesi md. 9 (x); 4. turun biçimi: PART I yalnız
  sayılı kümelerde ölçülen, PART II yalnız pinlerin listesi, PART III tek cümle):
  **(I) Sevk edilen kodun ölçülen davranışı, adıyla test ve küme.**
  (i) Rotalar: giriş, TOTP adımı, enrollment, çıkış ve `/operator`. 00026'nın beş
  definer'ının beşi de tenant okumaz; `/operator/tenants/{id}`, `/legal`, `/billing`,
  `/plaques`, `/audit` kayıtlı değil (`TestSurface_TheScreensOfLaterTasksAreNotMounted`) ve
  `screens()`'in 12 render'ı onlara link vermez
  (`TestOperatorScreens_EveryActionAndLinkIsAMountedRoute`).
  (ii) İki yönlü host kapısı, `httpx.OnHost` ile (istekteki port ve bir sondaki nokta düşer,
  harf büyüklüğü yok sayılır — `TestOnHost_ReducesTheRequestHostToTheConfiguredSpelling`'in
  satırları). Operatör yarısı `hostGate`: ingress manifestindeki host'larda ve altı host daha,
  yedi yöntem ve dokuz `/operator` yolunda router'ın kendi 404'ü (durum, gövde, `NotFound`'un
  yazdığı başlıklar), store çağrısı 0
  (`TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost`); chi'nin tanımadığı
  bir yöntem kök yönlendiricide 405, operatör yolu ve bilinmeyen yol için aynı cevap
  (`TestEscapes_CookieNamesDuplicatesExpiryAndOddMethods`). Müşteri yarısı `internal/httpx`'in
  `operatorHostOnly`'si: tablosunun satırlarında operatör host'unda `/operator…` ve `/static/`
  geçer, kök `/operator`'a 303, müşteri rotaları 404
  (`TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide`,
  `TestHostGate_TheOperatorHostServesNoCustomerRoute`). Kapalı ve ulaşılamaz hâllerin 503'ü
  sürülen host'larda değişmedi (`TestHostGate_TheUnavailableSurfaceKeepsItsAnswerOnEveryHost`).
  (iii) Zincir: konsol `hostGate → securityHeaders → floodGate → sameOriginGate →
  requireOperator → sessionGate`. Çerezsiz konsol isteği 303 ve store çağrısı 0
  (`TestSessionGate_NoLiveSessionIsASignInRedirect`); tek oturum, 101 adres, 101 istek → 100 ×
  200, 1 × 429, 101 yüklem çağrısı (`TestSessionGate_ABudgetPerSession`); bütçesi tükenmiş
  adresten canlı çerezle konsol → 429, store çağrısı 0
  (`TestFloodGate_AnExhaustedAddressReachesNoConsolePredicate`). Çıkış `sameOriginGate →
  requireOperator → logoutGate → op_close_session`: 3 001 çerezsiz çıkış → store çağrısı 0,
  ardından operatörün çıkışı oturumu kapatır
  (`TestLogout_ACookielessFloodCostsNothingAndCannotRefuseIt`); 3 000 çerezli çıkış →
  operatörün kendi çıkışı 429, çerez tarayıcıda silinir, oturum store'da canlı
  (`TestLogout_PastTheCeilingTheBrowserStillForgetsTheSession`; kart L16) — panelinkiyle aynı
  zayıf değişmez.
  (iv) `sameOriginGate`: güvensiz yöntemde `Origin` operatör origin'ine (`TAPPA_BASE_URL`'in
  şeması ve port'u + operatör host'u; varsayılan port düşer) harf büyüklüğü dışında eşit ya
  da `Origin` yok/`null` iken `Sec-Fetch-Site: same-origin` (panelin geri dönüşü `same-site`'ı
  da kabul eder — burada tehdit odur). Bir tarayıcının form POST'u `Origin: null` +
  `Sec-Fetch-Site: same-origin` taşıdı (1. tur denetçisi, headless Chrome). Ret 403, store
  çağrısı 0, bcrypt 0 (`TestSameOriginGate_ACrossOriginPostReachesNoStore`,
  `TestSurface_ACrossOriginPostPaysNothing`). Log *(4. tur, B6)*: 10 dakikalık, süreç geneli
  bir pencerenin ilk reddi bir WARN kaydı (yöntem; adres yok), diğerleri Debug — Info'da iki
  adresten 801 ret → 1 WARN kaydı ve 801'in tamamında store çağrısı 0 *(5. tur, N3: ölçüm
  önceden ilk 500'de duruyordu)*, ardından operatörün çıkışı aynı adresten oturumu kapatır
  (`TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel`). Konsolda
  `same-site`/`cross-site` getirme → 303, yüklem çağrısı 0
  (`TestSessionGate_ASameSiteReadDoesNotTouchTheSession`; getirme üst verisi göndermeyen
  tarayıcı kart L5).
  (v) Yanıt başlıkları *(4. tur, B1; 5. tur, F1/F2/N5–N7)*. Ölçülen nesne **WriteHeader
  anında yanıtın başlıklarıdır** (kaydedicinin `Result().Header`'ı — durum satırı yazılırken
  alınan kopya; ondan sonra eklenen ya da silinen bir başlık onu değiştirmez — telde ise
  net/http'nin belgelediği iki istisna var: 1xx yanıtlar ve trailer'lar; WriteHeader'dan sonra
  konan bir trailer kopyada değil `Result().Trailer`'dadır ve okunmaz). İki tablo, 48
  yanıt sınıfı: C1–C40 (`TestOperatorHeaders_FortyResponseClassesCarryThePolicy`) ve C41–C48 —
  `/operator`'da `HEAD`/`POST`/`OPTIONS`, `/operator/login/totp` ve `/operator/enroll`'da
  `PUT`, `/operator/logout`'ta `GET` (405) ve kod adımında ve enrollment'ta büyük form (413)
  (`TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy`). 15 düşmanca istek
  başlığıyla (`Location`, `Content-Security-Policy`, `Cache-Control`, `Referrer-Policy`,
  `X-Content-Type-Options`, `Set-Cookie`, `X-Frame-Options`, `Access-Control-Allow-Origin`,
  `Refresh`, `Referer`, `User-Agent`, `Accept-Language`, `X-Requested-With`, `HX-Current-URL`,
  `HX-Target`) ve operatörün iki çerez adını taşıyan ikinci bir `Cookie` satırıyla, 32'si
  (son isteği `/operator/login`, `/operator/login/totp` ya da `/operator/enroll`'a giden
  sınıflar: C1–C26, C38, C40, C44, C45, C47, C48) ayrıca düşmanca bir sorgu dizgisiyle
  sürüldüğünde, 48'inin her birinde durum ve son istek
  (`classRoutes`) tasarlanana, WriteHeader anındaki başlık adları tasarlanan kümeye ve
  değerleri tasarlanan değerlere eşit ölçüldü: CSP gövdesi betik yükleyen dört sınıfta (adıyla
  C18, C20, C21, C22) `enrollCSP`, 44'ünde `operatorCSP`; `Cache-Control: no-store`,
  `nosniff`, `no-referrer`; `Location`, `Content-Type`, `Allow` sınıfın tasarlanan değeri ya
  da yok; `Set-Cookie`'ler operatörün iki çerezinden, tasarlanan ayarla/sil durumunda ve
  öznitelikleriyle; 48'inin gövdesinde ve başlık değerlerinde düşmanca değer ham ya da
  sorgu-kaçışlı biçimiyle bulunmadı (testin aradığı iki biçim); altı 405'te store çağrısı 0.
  Kopyanın teldeki başlık bölümüyle (net/http istemcisinin ayrıştırdığı `Response.Header`) aynı
  olduğu üç sınıfta (C1, C18, C28) gerçek bir `httptest.Server` üzerinden ölçüldü,
  `Content-Length` ve `Date` adıyla dışarıda; trailer bölümü, hijack edilmiş bağlantı ve
  sunucunun öbür çerçeve başlıkları (`Transfer-Encoding`, `Connection`) karşılaştırılmadı
  (`TestOperatorHeaders_TheRecorderSnapshotIsWhatTheWireCarries`). `chi.Walk`'un bildirdiği
  monte edilmiş her yöntem × rota çiftinin ve her rotanın bir 405'inin `classRoutes`'ta bir
  sınıfı var (`TestOperatorHeaders_TheWalkedRoutesEachHaveAClass`).
  (vi) Çapraz çerez, gerçek çözümleyicilerle ve gerçek oturumlarla: müşteri tarafının
  yöneticilerince verilmiş bir panel ve bir çalışan oturumunun değeri operatör çerez adıyla
  `op_touch_session`'a istek başına tam 1 çağrıyla ulaşır ve 303'tür; operatörün canlı
  token'ı panelin ve çalışanın çerez adıyla müşteri çözümleyicilerinde reddedilir, her biri
  tam bir kez sorgulanarak (`TestE2E_CrossCookie_NeitherSideAcceptsTheOthersValue`).
  (vii) Operatörün iki çerezi `/legal/cookies`'te bilinçli olarak yok (operatör host'unun
  `__Host-` çerezleri, Taptime personeli için); çerez taraması onları görür ve dışarıda
  bırakmayı dosyalarına bağlar (`TestCookieNotice_ListsExactlyTheCookiesTheProductSets`,
  `TestCookiesNotOnTheNotice_AreBoundToTheirFile`).
  (viii) Sorgu dizgisindeki kimlik bilgisi *(4. tur, B2; 5. tur, N4 — zaman kipi ölçülene
  eşitlendi)*: testin sürdüğü isteklerde sorgudaki parola/adres giriş sayılmadı
  (`TestSignIn_ReadsTheBodyNeverTheQuery`), sorgudaki token sayfada ham biçimiyle bulunmadı
  (`TestEnroll_TheTokenNeverTravelsInTheURL`); sızıntı testinin A28/A29 kollarında (R1–R10
  render'larıyla) ve (v)'deki 32 sınıfın düşmanca sorgusu yanıtlarda ham ya da sorgu-kaçışlı
  biçimiyle bulunmadı *(6. tur, B1/N-1)*.
  (ix) Zayıf parola *(4. tur, B7)*: 14 karakterden kısa, 72 bayttan uzun ve UTF-8 olmayan
  parolanın üçü tek uyarı alır, uyarı iki sınırı da söyler
  (`TestEnroll_TheWeakPasswordNoticeNamesBothLimits`).
  **(II) Pinler:** yukarıdaki testler ve go/types pinleri —
  `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` (SC1–SC6),
  `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` (PV1–PV7),
  `TestEnrollScreen_TheListedFormsRenderItOnlyInRenderEnroll` (EN1–EN3),
  `TestFormValues_TheListedSitesAloneRevealOrReadTheForm` (FV1–FV7),
  `TestClientAddress_TheListedReadsFeedOnlyTheBudgets` (AD1–AD5),
  `TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions` (RH1–RH5; 5. turda RH3'e
  `http.Header` üstünde yerleşik `delete`/`clear` eklendi),
  `TestHostGate_TheListedHostReadsOccurOnlyThroughOnHost` (HG1–HG3),
  `TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders` (SN1–SN2),
  `TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell` (IM1–IM3),
  `TestOperatorHostFile_ImportsOnlyThreeStandardPackages`. Her pinin yakaladığı liste testin
  başlığındadır ve pinin iddiası o listedir. Mutasyon tabloları kartta (2. tur 69, 3. tur 51,
  4. ve 5. tur alt blokları).
  **(III)** Bu nottaki ölçümler adıyla geçen testlerin sürdüğü kümelerdir ve pinler yalnız
  kendi listelerini yakalar; bu küme ve listelerde olmayan her biçim (örnekler: 48 sınıfın
  dışındaki bir yanıt, HTML-kaçışlı ya da base32 bir yansıma, trailer bölümü, hijack edilmiş
  bağlantı, sunucunun çerçeve başlıkları, `encoding/asn1` ile kurulan bir `ProblemView`,
  `r.TLS.ServerName`, `URL.RequestURI()`, `X-Cluster-Client-IP` başlığı, başka paketteki bir
  yardımcı) kod incelemesinin konusudur — tamlık iddiası yok.
  **Sınırlar:** sayılı sınırlar kartta (OP-8 bloğu, L1–L21). 6. turda eklenen ikisi: **L20** —
  girişin bcrypt'i için süreç geneli tavan yok, yalnız adres başına `work` bütçesi (20/10 dk);
  dağıtık bir istemci paylaşılan süreçte CPU'yu doyurabilir (panelin `/admin/login`'i aynı
  sınıfta ve bugün canlıda erişilebilir; operatör host'u canlıda erişilemez); devir OP-9, K4.
  **L21** — araya sokulan aynı adlı bir çerez operatörü tarayıcıdan atabilir: sunucu yarısında
  ilk çerez okunur ve çöp-önce istek 303'tür; tarayıcı yarısı doğrulanamadı; sonucu zorla
  çıkıştır, erişim vermez.

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
  **OP-14 D notu (2026-10-07, migration 00033) — TEK istisna, adıyla: platform sahibinin
  `cmd/opadmin` satırları.** §6'nın üç eylemi (`create`, `reset-mfa`, `disable`) birer satır
  yazar — `operator_created`, `operator_mfa_reset`, `operator_disabled` — ve bu satırlar bir
  definer'dan **değil**, sahibin (`tappa_owner`) psql ile uyguladığı SQL'in hesabın işini yapan
  aynı `DO` bloğundan gelir (iş ve iz tek ifade: birlikte commit edilir ya da hiçbiri).
  **Gerekçe:** hesabı yaratan, sıfırlayan ve kapatan yazı zaten sahibindir (aşağıda §6:
  `platform_admins`'e yalnız sahip yazar); sahip tablonun sahibi ve dağıtılan topolojide süper
  kullanıcıdır, yani satırlar ona bir güç değil bir iz ekler. Bir definer **seçilmedi**: sahip
  türü yazan bir `op_*`, `tappa_operator`'ın çağırabildiği bir fonksiyon olurdu — DSN sahibinin
  herhangi bir hesap için *"platform sahibi bu hesabı kapattı"* basabildiği bir kapı.
  **Şekil:** `operator_audit_log_actor_shape`'in **üçüncü kolu** — oturum yok, aktör yok, hedef
  hesap (`target_admin_id`) var, ve başka hiçbir şey (tenant, kapsam, sayfa yok; `detail` tam
  olarak `{}`): satır hesabı **id'siyle** adlandırır, adres ve ad audit'e girmez ve şema bunu
  zorlar. `op_record_auth_event`'in kapalı kümesi bu türleri adlandırmaz; `tappa_operator`
  tabloya yazamaz. Sınırlar ve ölçümler: ADR 0021 "OP-14 D uygulama notu".
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
  transaction'da satırı geriye tarihler — ölçüldü, ADR 0021 §2 vii). **OP-14 D notu
  (2026-10-07, 00033):** *"INSERT listesinde yoktur"* tanımlayıcı için doğruydu, sahip için
  değil — sahip `at`'e yazabiliyordu (ADR 0021 OP-14 notu md. 7, sınır L2). 00033'ten beri bir
  `BEFORE INSERT` tetikleyicisi **her** yeni satırın `at`'ini duvar saatine zorlar — kim yazarsa
  yazsın, sahip ve `COPY` dahil. Sahip tarih ancak tetikleyiciyi **kapatarak, düşürerek ya da
  fonksiyonunu değiştirerek** (ya da süper kullanıcının `session_replication_role = replica`
  oturumuyla — belgeden, ölçülmedi) seçebilir: ilk üçü **sahiplik** ister — tablonun ya da fonksiyonun; süper
  kullanıcılık değil (PostgreSQL belgesi) — ve ikisinin de sahibi `tappa_owner`'dır (ADR 0021
  sınır 5). *(2. tur düzeltmesi: ilk yazım yalnız `DISABLE`'ı ve
  "süper kullanıcı yolu"nu sayıyordu.)*
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
  **OP-8 notu (2026-10-02): biçim SEÇİLDİ — fragment.** Link
  `https://<operatör host'u>/operator/enroll?id=<hesap id>#<token>`'dır: id sorguda (sır
  değil; GET sayfanın anahtarını o hesaba mühürlemek için ona ihtiyaç duyar; ürünün erişim
  kaydı rota desenini yazar, sorguyu değil — ingress'inki yazar), token fragment'ta (tarayıcı
  göndermez). Sayfanın betiği (`web/static/js/operator/enroll.js`) token'ı forma koyar,
  alanı gizler ve fragment'ı `history.replaceState` ile adres çubuğundan siler; betiksiz
  tarayıcıda alan görünür kalır ve kişi `#`'den sonrasını yapıştırır. Sorguya konan token
  `TestEnroll_TheTokenNeverTravelsInTheURL`'un sürdüğü istekte sayfada ham biçimiyle, iki
  başlık tablosunun sorgu taşıyan 32 sınıfında yanıtta ham ya da sorgu-kaçışlı biçimiyle
  bulunmadı (§4 OP-8 notu (I)(viii)). Yol parçası
  seçilmedi: ingress yolu log'lar. OP-9 linki bu biçimde basar; ingress'in neyi log'ladığının
  ölçümü OP-9'da kalır. *(2. tur — betiğin davranışı hakkındaki iddia üç parçaya çekildi:)*
  **(I)** sevk edilen betiğin pinli olan METNİDİR: kodunun normalleştirilmiş sha256'sı
  (`TestEnrollScript_IsTheReviewedBody`; kodda bir değişiklik özeti değiştirir) ve adlar listesi
  (`TestEnrollScript_TouchesTheFragmentAndNothingElse`); DAVRANIŞI 2026-10-02'de 1. tur denetçisince
  headless Chrome'da elle ölçüldü (geçerli token → alan 43 karakter ve link token'ına eşit,
  sarmalayıcı gizli, `location.hash` boş, fragment ölçülen isteklerde yok; bozuk fragment → alan
  boş ve görünür, fragment silindi) ve **pinli değildir** — depoda JavaScript motoru yok ve
  eklenmedi. **(II)** iki pin, yukarıda; yakaladıkları: kodun özeti ve kodun kullandığı adlar.
  **(III)** Bu iki pin yalnız metni yakalar; davranışı değiştiren her şey (örnekler: bir
  tarayıcı sürümü, aynı metnin başka bir sayfada yüklenmesi) kod incelemesinin ve elle
  ölçümün konusudur — tamlık iddiası yok (kart L15).
- **`platform_admins`'e yalnız `tappa_owner` INSERT eder** (ADR 0021 §1). HTTP'den ya
  da uygulama rollerinden operatör yaratmak imkânsızdır; tablo boşsa kimse giremez.
- **Reddedilen:** env'den tohumlama — bir kimliği bir dağıtımın yan etkisi yapar ve bir
  kimlik bilgisini bir yapılandırma dosyasına taşır (A-0'ın sınıfı: sır bir kayıt
  dosyasında). Herkese açık kurulum sayfası — tablo boşken herkese açık bir yazma yolu,
  yarışı kazananı platform operatörü yapar.
- **OP-9 notu (2026-10-02): uygulandı — `cmd/opadmin`.** Ayrıntı ve ölçümler:
  [m10-platform.md](../plan/m10-platform.md) → *"Kart düzeltmesi (2026-10-02, OP-9
  uygulaması sırasında)"*; runbook `deploy/README.md` → *"Operator accounts (M10 OP-9)"*.
  Bu bölüme eklenen kararlar: (a) link sırrı `operatorauth`'un sözleşmesiyle (256 bit,
  43 karakter base64url, anahtarsız SHA-256 hex) **`cmd/opadmin`'de** basılır, paket import
  edilmez — `go list -deps ./internal/operatorauth` pgx ve `database/sql` listeliyor
  (ölçüldü); eşitlik testten pinli. (b) `reset-mfa` ve `disable` hesabı **id VE e-posta**
  ile bulur. (c) `reset-mfa` yukarıdaki tanıma ek olarak **parola özetini de siler**
  (enrollment yenisini yazar) ve şunları reddeder: `disabled` hesap; hash'i zaten bu
  betiğinki olan hesap (ardı ardına ikinci uygulama); ve — 2. tur, A-B-A — betik
  üretildikten **sonra** link verilmiş hesap (`enroll_issued_at` ≥ betiğin üretim zamanı).
  `create` ve `reset-mfa` üretimden 30 dk sonra ve üretim zamanı veritabanı saatinin
  önündeyse (3. tur: kodda pay yok — ölçülen en küçük ret +250 ms; 2. turun 30 sn payı
  ölçülmüş bir tekrar penceresiydi)
  reddedilir; korumanın iki saati karşılaştırmaktan kalanı `deploy/README.md` sınır O9-5'te. (d) SQL kendi `BEGIN … COMMIT`'ini taşır, hesabın işi tek
  bir `DO` bloğunda; **2. tur:** link sırrının hash'i, e-posta ve ad ifade metninde değil
  **`COPY … FROM STDIN` veri satırında** gider — §5'in *"enrollment token'ı HİÇBİR YERDE
  loglanmaz … ham hâlleri de hash'leri de"* kuralı, sunucu log'unun yazdığı ifade metnine karşı; ifade metninde hesap id'si,
  dolgu, üretim zamanı ve sabitler kalır; veri satırında değerlerden önce 100 baytlık sabit bir
  dolgu durur (bir COPY hatası satırın ilk 100 baytını CONTEXT'e yazar — ölçüldü); ölçülen
  istemci kipleri: psql dosyayı okurken (stdin, `-f`, `\i`) — `psql -c` dosyayı tek ifade
  olarak gönderir ve hash başarısız ifadenin metniyle log'a düşer (ölçüldü). Ölçüm ve kapsamı:
  README sınır O9-3. (e) opadmin'in
  eylemleri `operator_audit_log`'a yazılmaz (tür kümesi kapalı — migration ister; OP-14'e,
  adıyla; README sınır O9-1). *(OP-14 D, 2026-10-07, 00033: kapandı — her eylem aynı `DO`
  bloğunda bir audit satırı yazar; §5'in OP-14 D notu.)* (f) Ingress ölçümü (§6'nın OP-9'a bıraktığı): Go `net/http`,
  curl ve headless Chrome linkin fragment'ını ne istek satırında ne `Referer`'da gönderdi;
  ingress satırı hesap id'sini taşır.

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
- **OP-10 A fazı notu (2026-10-03).** Veri katmanı uygulandı:
  `db/migrations/00027_move_legal_publishing_to_the_operator.sql`. Tablo düzeyi
  `REVOKE INSERT ON legal_documents FROM tappa_app` ölçü sözleşmesiyle ölçüldü (dev, `BEGIN …
  ROLLBACK` ve sonra canlı katalogda: `has_any_column_privilege` `t` → `f`, `slug`/`body`/
  `published_by` INSERT üçü de `f`, SELECT (id, slug, body, published_at) `t`); sütun düzeyi
  yazım (`REVOKE INSERT (slug, body) …`) `has_any_column_privilege`'ı `t` bıraktı.
  `op_publish_legal(p_session, p_slug, p_body)` `void` döndürür, `published_by`'ı oturumun
  operatöründen alır, 256 KiB'ı `octet_length(body) > 262144` ile uygular; sürüm listesi ilk
  `op_read_*`'tır (`op_read_legal_versions`) ve eski satırları `publisher_kind = 'legacy'`
  olarak, müşteri admin id'si olmadan döndürür. `published_at`'in DEFAULT'u
  `clock_timestamp()` oldu. Ekran, `legal.Store.Refresh` wiring'i ve "Tamamen kaldırılanlar"
  listesi B fazındadır. Ayrıntı: [ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md) →
  "OP-10 uygulama notu".
- **OP-10 B fazı notu (2026-10-03).** Ekran, wiring ve emeklilik uygulandı (ayrıntı ve
  ölçümler: [m10-platform.md](../plan/m10-platform.md) → OP-10B kart düzeltmesi):
  `internal/handler/operator/legal.go` — `GET`/`POST /operator/legal`, konsolun grubunda
  (host kapısı → güvenlik başlıkları → flood → same-origin, okuma kapısıyla →
  `requireOperator` → `sessionGate`); `operatorpages.Legal`; `*db.OperatorDB`'ye
  `LegalVersions`/`PublishLegal` (handler paketindeki `operator.LegalStore`);
  `operatorauth.(*Authenticator).SessionHash` (handler'ın oturum taşıyan `op_*` çağrıları
  için, `Verify`'ın store'a verdiği değer). Kaldırılanlar §7'nin listesidir: `TabLegal`,
  `PanelSection.OperatorOnly`, `PanelChrome.Operator`, `mayPublishLegal`,
  `config.OperatorAdminIDs`, `TAPPA_OPERATOR_ADMIN_IDS` (`deploy/k8s/05-config.yaml`,
  `.env.example`, `deploy/README.md`), `internal/handler/legaladmin.go`, panelin
  `legaladmin.templ`'i, `legal.Store.Publish`/`Trail`/`ActionPublished`/`PublishedDetail`,
  `db/queries/legal.sql`'in `PublishLegalDocument`'ı; `legal.NewStore(data)`. **K11:**
  KF hesabı panelde sıradan müşteri. Bu bölüme eklenen kararlar: (a) yayın formu
  `slug` ve `body` taşır; yayımlayan oturumdan gelir, formdaki başka alanlar okunmaz;
  (b) gövdenin uçları kırpılır, içi yazıldığı gibi saklanır; görünür karakter kuralı:
  Unicode L/N/P/S kategorilerinden, `Other_Default_Ignorable_Code_Point` ve adı konmuş üç
  boş sembol (U+2800, U+303F, U+1D159 — üçü de dev veritabanında `[:space:]` değil, ölçüldü)
  dışında en az bir karakter; U+FFFC görünür sayılır (karar: gömülü nesnenin yerini tutan
  görünür bir glif — Unicode adından, çizim ölçülmedi) (OP-10A'nın ölçtüğü sekiz kod
  noktasının sekizi de reddedilir);
  (c) 256 KiB istek gövdesine (`MaxBytesReader`, kodlanmış form) uygulanır — çözülen
  değer kodlamasından uzun olamaz; (d) yayın ve ardından `legal.Store.Refresh` istek
  bağlamından KOPUK koşar (`context.WithoutCancel` + 10 sn zaman aşımı): bağlantıyı yayından
  sonra kapatan bir tarayıcı, sürüm commit olmuşken tazelemeyi iptal ettiremez. Tazeleme
  hatası loglanır ve yanıt yine 303'tür (POST → 303 → GET: yeniden yükleme ikinci kez
  yayımlamaz); ekran her açılışta anlık görüntüyü (listeden ÖNCE okunur: önce okunan anlık
  görüntü sonra okunan listeden yeni olamaz) listenin canlı sürümüyle karşılaştırır (yayın
  zamanı ve bayt uzunluğu), farklıysa bir kez tazeler; uyarı yalnız anlık görüntünün
  `published_at`'ı canlı sürümünkinden ÖNCE ise ya da (eşit zamanda, id'nin vekili olarak)
  uzunluğu farklıysa çıkar (iyileştirmenin kurduğu, daha geç yayımlanmış bir sürüm uyarı
  vermez). Veritabanının sırası `(published_at DESC, id DESC)`'tir; anlık görüntü id taşımaz,
  bu yüzden aynı slug'ın iki yayını aynı mikro saniyeye düşerse karşılaştırma iki yönde
  yanılır — bir görüntülük yanlış uyarı ya da aynı uzunlukta başka metinde ne tazeleme ne
  uyarı (kart LB11; kesin düzeltme `legal.Doc`'a `ID`, yazılmadı) — o
  belgenin editörü *"The public page is behind"* uyarısını canlı sürümün zamanıyla gösterir — editör
  sayfanın metnini taşır, yayımlamak en yeni sürümün yerine geçer; yayının KENDİ hatası 503'tür
  ve sayfası satırın yazılmadığını iddia etmez (bir hata bunu kanıtlamaz — zaman aşımının
  iptali, COMMIT'ten sonra kopan bağlantı), sürüm listesine bakmayı söyler; (e) okuma oturum bütçesine
  iki birim sayılır (`sessionGate` + ekran), `sessionLimit` 100 korundu *(OP-13 notu,
  2026-10-06: bu, OP-10'un bütçesinin kaydıdır. OP-13 B'den itibaren okuma ayrı bir
  `readLimit`'e (60 / 10 dk) handler'da bir birim sayılır; `sessionLimit` 100'dür ve yalnız
  istek sayar — okumanın ikinci birimi artık oturum bütçesinin değil. Bkz. ADR 0021 →
  "OP-13 B fazı eki" md. 8.)*; (f) sürüm listesi
  en yeni 100 sürüm, gövdesiz, yayımlayan operatörün adı ya da *"tenant admin (legacy)"*;
  geri alma eski metni yeniden yayımlamaktır (yeni satır), "bu sürüme dön" düğmesi yeni bir
  `op_read_*` ister ve bu görevde yok. ADR 0016 Sonuçlar'ın *"bu metni kim yayımladı
  sorusunu cevaplayan bir ekran yok"* maddesi bu sürüm listesiyle kapandı (`op_*`
  üzerinden; `tappa_app` `published_by`'ı hâlâ okuyamaz).

  **Güvenlik iddiası — üç parça.**
  **(I) Sevk edilen kodun ölçülen davranışı, adıyla test ve küme.** (i) `/operator/legal`'in
  18 yanıt sınıfı (C49–C66) 15 düşmanca istek başlığı, ikinci `Cookie` satırı ve düşmanca
  sorgu dizgisiyle sürüldü: WriteHeader anında durum, `Location`, başlık adları ve değerleri
  tasarlanana eşit; düşmanca değer gövdede ve başlık değerlerinde ham ya da sorgu-kaçışlı
  biçimiyle bulunmadı; çapraz-origin yayında (C58) ve `PUT`'ta (C66) store çağrısı 0, ikinci
  bütçe biriminin reddettiği okumada (C55) `LegalVersions` çağrısı 0
  (`TestOperatorHeaders_TheLegalClassesCarryThePolicy`). (ii) Operatör host'u dışında
  `/operator/legal` yedi yöntemde router'ın kendi 404'ü, store çağrısı 0
  (`TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost`; yolları onu
  içerir). (iii) Sızıntı sözleşmesinin A31–A43 kollarında (sözleşmenin on gösterimi, dört
  yüzeyi) oturum hash'i (G8) ve yasal form değeri (G16: gönderilen metinler, bilinmeyen
  slug) tasarlanmış çıkış D6 dışında bulunmadı — D6: ekranın editörleri anlık görüntünün
  metnini gösterir (`TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`). Okuma
  bileti `internal/db`'nin içinde üretilip tüketilir ve `LegalStore`'un imzalarında yok
  (kaynak okundu); sızıntı testinin kollarında aranacak bir değeri yok. (iv) Yayın gönderen oturumun hash'iyle gider; formdaki
  `published_by`, `admin_id` ve `session` alanları başka birini adlandırırken yayımlayan
  oturumun operatörüdür; anlık görüntü yayın başına bir kez tazelenir; geri alma üçüncü
  sürümdür (`TestLegalPublish_TheSessionPublishesAndThePublicSnapshotFollows`). (v) Dokuz
  slug biçimi (`TestLegalPublish_RefusesASlugTheProductDoesNotHave`), 24 görünmez gövde
  (`TestLegalPublish_RefusesAnEmptyBodyAndSaysSo`, `TestVisibleText_TheListedInvisibleBodiesAreRefused`)
  ve 256 KiB + 1 baytlık istek (`TestLegalPublish_RefusesABodyBiggerThanTheCeiling`) store
  çağrısı yapmadan reddedildi; tam 256 KiB yayımlandı. (vi) Okuma iki birim: tek oturum 50
  görüntü → 50 × 200, 51. 429 (`TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget`).
  *(OP-13 notu, 2026-10-06: test adını korur; OP-13 B'den beri okuma ayrı bir `readLimit`'e bir
  birim sayılır ve test 60 görüntü → 60 × 200, 61.'si okuma bütçesinden 429, ardından oturum
  bütçesini (`sessionLimit` 100) ölçer. Bkz. ADR 0021 → "OP-13 B fazı eki" md. 8.)*
  (vii) Gerçek Postgres'te: yayın → bir sürüm (`published_by` = operatör) ve bir
  `legal_publish` satırı (detail'de slug ve bayt, metin değil) → müşteri host'unda
  `/legal/privacy` yeni metni gösterir → sürüm listesi yayımlayanın adıyla, canlı → geri
  alma yeni satırdır, yayımlanan sürüm durur; iki görüntü iki `read` satırı ve tüketilmiş iki
  bilet; yayından ve geri almadan sonra ekran "geride" demez — Postgres'in kendi tiplerinde
  anlık görüntü listenin canlı sürümünden ESKİ değil (iddia bu kadar: uyarı tek yönlü
  olduğundan listeden bir mikro saniye yeni bir anlık görüntü de uyarı vermez, yani iki
  zamanın EŞİTLİĞİNİ bu test ölçmez)
  (`TestE2E_LegalPublishRefreshesThePublicPageAndTheListNamesThePublisher`);
  MFA'sız, iptal edilmiş, 31 dk boşta ve 8 saati geçmiş dört oturumla yayın 303; NUL baytlı
  metin 503, 256 KiB üstü 413, bilinmeyen slug ve görünmez metin 400; ardından operatörün
  sürümü ve `legal_publish` satırı 0 (`TestE2E_LegalPublishRefusesDeadSessionsAndBadTextsWritingNothing`).
  (viii) Müşteri panelinin dokuz bölümü sahip ve yönetici olarak altı operatör işareti
  taşımadı ve `/admin/legal` GET/POST 404 (`TestCustomerPanel_EverySectionCarriesNoOperatorElement`;
  gerçek kayıt olmuş bir müşterinin paneli aynı listeyle `adminlogin_db_test.go`'da).
  (ix) Herkese açık yol: `marketing.go` `Refresh`/`Publish`/`PublishLegal`/`Paragraphs`
  çağırmaz, sayfalar anlık görüntüyü okur, refresh 0 (`TestLegalPublicPath_WritesNothing`);
  okuyucunun tek yöntemi bağlamsız ve hatasız (`TestLegalReader_CannotReachTheDatabase`).
  (x) İstemci handler'dan ÖNCE ve sürüm kaydedildikten hemen SONRA ayrıldığında (istek
  bağlamı iptal) yayın saklanır ve anlık görüntü yeni metni sunar; her tazelemenin bağlamı
  iptal edilmemiş ve ≤ 10 sn son tarihlidir; kontroller: istek bağlamı gerçekten iptal
  edildi, sahte store ve anlık görüntü iptal edilmiş bağlamı reddeder
  (`TestLegalPublish_AClientThatLeavesStillGetsThePublicationAndTheRefresh`). (xi) Tazelemesi
  başarısız yayın 303; ardından ekran 200, bir tazeleme dener, uyarıyı bir kez ve canlı
  sürümün zamanıyla gösterir, editör sayfanın sunduğu metni taşır; tazeleme düzelince sonraki
  görüntü anlık görüntüyü iyileştirir (bir tazeleme, uyarı yok), ondan sonraki görüntü
  tazelemez; uyarının zamanı Notice bloğunun içinde canlı sürümünkidir, anlık görüntününki
  değil (sahte sürümler bir dakika arayla); kontrol: güncel anlık görüntüde uyarı yok, tazeleme
  yok (`TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind`; sızıntı kolu A43
  aynı dalın gövdesini ve log satırını arar). (xii) İki okumanın arasına giren bir yayın (ve
  tazelemesi): görüntü 200, uyarı yok, kendi tazelemesi yok, editör listenin canlı gösterdiği
  sürümü taşır (`TestLegalPage_AVersionPublishedBetweenTheTwoReadsIsNotCalledBehind`);
  iyileştirmenin tazelemesi listeden yeni bir sürüm kurduğunda uyarı yok
  (`TestLegalPage_ASnapshotNewerThanTheListAfterTheHealIsNotCalledBehind`).
  **(II) Pinler:** `TestOperatorDB_IsTheStoreAndNothingMore` (yöntem kümesi
  `operatorauth.Store` ∪ `operator.LegalStore` ∪ `Close`), `TestOperatorDB_EveryMethodDelegatesVerbatim`
  (dokuz yöntem), `TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor` (öncül 10),
  `TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator` (havuz → `configuredSurface` ve
  `Close`; store → `operatorAuthenticator` ve `operator.New`'in ikinci argümanı; `texts` →
  `configuredSurface` → `operator.New`'in üçüncü argümanı; `legal.NewStore` komutta bir kez ve
  `run()`'ın bağladığı değer tam olarak açılış `Refresh`'inin alıcısı, `openOperatorSurface`'in
  üçüncü ve `handler.NewMarketing`'in ilk argümanı — operatörün tazelediği anlık görüntü
  herkese açık sayfaların okuduğudur; `run()` yüzeyi
  `httpx.NewRouter`'a verir), `TestFormValues_TheListedSitesAloneRevealOrReadTheForm` (FV5:
  `publishLegal`'de `.Get("slug")` ve `.Get("body")` birer kez),
  `TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions` (RH4: yönlendirme hedefi
  `pathLegal` dahil monte edilmiş sabitler), `TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders`
  (yedi ekran), `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass` (C1–C66; altı rota, on
  çift), `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` (on üç değişken).
  Her pinin yakaladığı liste testin başlığındadır.
  **(III)** Bu nottaki ölçümler adıyla geçen testlerin sürdüğü kümelerdir ve pinler yalnız
  kendi listelerini yakalar; listede olmayan her biçim kod incelemesinin konusudur — tamlık
  iddiası yok.
  **Sınırlar:** kartta (OP-10B bloğu, LB1–LB11; LB10: iki operatör arasında kaybolan güncelleme — düzeltmesi migration ister; LB11: aynı mikro saniyede iki yayın — karşılaştırma id taşımaz).

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
| *"Tenant sınırının aşıldığı her yer, kodda ve ekranda adıyla görünür — sessiz bir çapraz-tenant okuma yok."* | Kodda: yalnız `op_` önekli fonksiyonlar (ADR 0021). Ekranda: tenant-ötesi her ekran girdiği tenant'ı başlıkta **adıyla** gösterir (OP-8). *(OP-11: liste tek bir tenant'a girmez ve başlık taşımaz, her satır tenant'ı adıyla gösterir; görünür adı olmayan bir tenant hem satırda hem genel bakışın başlığında "Unnamed tenant" ve id'siyle adlandırılır — sıfır `TenantName` hâlâ render edilemez.)* *(OP-13: plaket ekranı da `TenantScreen` içindedir; başlıktaki ad envanter okumasının kendisinden gelir — ayrı bir genel bakış okuması yapılmaz — ve okumanın döndürdüğü tenant yolun tenant'ı değilse ekran 503'tür.)* *(OP-14: audit görüntüleyicisi bir tenant'a **girmez** — günlük her operatörün ve bir sayfası birçok tenant'ı adlandırır —, bu yüzden "girdiği tenant'ı başlıkta" cümlesi ona şöyle okunur: başlık banner'ı yok, düz operatör kabuğu (`screen`); satırın dokunduğu her tenant satırda **adıyla** (`bdi`, kaçışlı) ve genel bakışına linkle durur; görünür adı olmayan tenant "Unnamed tenant" ve id'siyle, hiçbir tenant'ın taşımadığı bir id "A tenant id no tenant has" ve id'siyle, linksiz. ADR 0021 → "OP-14 B fazı eki" md. 4.)* *(OP-12: faturalama ekranı da `TenantScreen` içindedir; başlıktaki ad fatura okumasının kendisinden gelir — ayrı bir genel bakış okuması yapılmaz — ve okumanın cevabı istenen tenant'ın istenen sayfası değilse ekran 503'tür. ADR 0021 → "OP-12 B fazı eki" md. 3–4.)* Sessizlik: okuma audit'i veri dönmeden commit edilir (§5) |
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
  → **OP-6 (2026-09-26): N = 5 ve 15 dk KORUNDU** (migration yok). Aritmetik: ±1 pencere
  tahmin başına 3/10⁶; kilit şekli ilk kilitten sonra pencere başına en çok bir tahmin →
  parolayı zaten bilen ve bir yıl boyunca her pencerede deneyen biri için ~35 000 tahmin ≈
  %10/yıl, her tahmin bir `totp_failed` satırıyla. Daha sıkı bir şekil (artan pencere)
  migration ister — açık, orkestratörün kararı.
- Ara çerezin ömrü ve üç limiter'ın sayıları — OP-6/OP-8 (panel limiter'larının
  aritmetik savunması emsal). → **OP-6'da karara bağlandı (2026-09-26):** ara çerez
  (`__Host-taptime_op_login`) 5 dk + 1 dk geri saat toleransı, anahtarı
  `TAPPA_OPERATOR_TOKEN_HMAC_KEY`'den etiketle türetilir (müşteri anahtarından değil — §2'nin
  yasağının ruhu korunur); bütçeler flood 300 · iş 20 · hesap 10 (TOTP'de kapı) · süreç
  geneli parolasız audit tavanı 30 · süreç geneli enrollment 10 · *(2026-10-01, 12c)* adres
  başına enrollment payı 3 (süreç geneli enrollment'tan önce) · *(2026-10-07, OP-14 C)*
  operatör başına `password_ok` satır tavanı 10 (bir satır tavanı: tavanın üstündeki doğru parola
  yalnız penceresinin **yazılmış** satırları tavana ulaşmışsa satırsız hizmet görür, ulaşmamışsa
  adım fail-closed reddedilir, 503 — OP-14 C 3. tur F1, cümle 5. turda daraltıldı), hepsi 10 dk.
  Gerekçeler ve ölçümler: [m10-platform.md](../plan/m10-platform.md) → OP-6 kart
  düzeltmesi, `internal/operatorauth/limits.go`.
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
  kartı (OP-15/OP-16). → **OP-16 A'da karara bağlandı (2026-10-07, 00034): v1'de
  GÖSTERİLMEZ, çünkü gösterecek yüzey yok.** İlk değiştiren `op_*` (`op_record_vat_check`)
  tenant'ın `audit_log`'una `tenant.vat_rechecked` yazar (`actor_id` operatör,
  `detail.actor_kind = "operator"`); müşteri tarafında `audit_log` okuyan iki sorgu bunu
  döndüremez — `ListPlaqueHistory` yalnız `action LIKE 'plaque.%'`, `ConfirmRecentRemoval`
  yalnız `location.deleted` ve oturumdaki yöneticinin satırı (sorgu metni okundu). OP-15'in
  `tenant.suspended`/`tenant.reinstated`'ı da `tenant.*`'tır, aynı hüküm. **OP-18'e devir:**
  operatör `plaque.*` yazdığında `ListPlaqueHistory`'nin `admin_users` LEFT JOIN'i satırı
  *"adsız yönetici"* okur (`by_system = false`, ad `''`) — o kart `actor_kind`'ı okumalı. ADR
  0021 "OP-16 uygulama notu", LV9.

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
  rolünün parolası. → **OP-7 notu (2026-10-01):** liste **dört** addır ve parolası onlardan
  biri **değildir**: `TAPPA_OPERATOR_DATABASE_URL`, `TAPPA_OPERATOR_TOTP_KEK`,
  `TAPPA_OPERATOR_TOKEN_HMAC_KEY`, `TAPPA_OPERATOR_HOST` (sır olmayan host da Secret'tan
  gelir: ConfigMap her deploy'da yeniden uygulanır, anahtarlar yokken orada duran bir host
  yarım küme olurdu). Rolün parolası yalnız DSN'in içindedir ve role psql'in `\password`'üyle
  stdin'den verilir (`deploy/README.md` → *"Operator surface (M10 OP-7)"*); ayrı bir anahtar
  ancak onu bir pod tüketseydi gerekirdi, ve `10-postgres.yaml`'a böyle bir `secretKeyRef`
  OP-5 md. 15'in uyarısına çarpardı.
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
