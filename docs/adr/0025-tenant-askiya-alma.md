# ADR 0025 — Tenant askıya alma: yazma kilidi, kayıt sürer

- **Durum:** **Kabul edildi (2026-10-08; kullanıcı kararları OP-K5 = (a), K-B1 = (i))** — iki kararın
  metni ve gerekçesi *"Karar"*ın başında, *"Kullanıcı kararları"*nda. **OP-15'in A fazı (veri katmanı)
  başlayabilir.** **Uygulama: yok** — bu ADR yazıldığında (HEAD `3f55d16`) şemada askı sütunu, askı
  türü ya da askı fonksiyonu yoktur ve panelde askı kapısı yoktur (aşağıda *"Bağlam"*).
- **Tarih:** 2026-10-07 · aynı gün **2. tur** (üçüncü gözün RED'i: üç bloklayan, yedi bloklamayan
  metin bulgusu; orkestratörün iki kararı — encode rölesine yeni bir `suspended` sözcüğü ve tek kaynak
  olarak `tenants`'ın iki sütunu) · 2026-10-08 **3. tur** (dar kapanış ONAY'ı; bir orta ve dört düşük
  bulgu — orkestratörün kararı: OP-16A'nın sırası, önce satır kilidi sonra saat) · aynı gün **4. tur**
  (güvenlik okumasının RED'i: bloklayan bulgu kullanıcıya soruldu — K-B1; üç bloklamayan bulgu —
  orkestratörün kararları: ortak yönlendirici kurucusu, `security_hold`'un nötr cümlesi ve operatör
  uyarısı, genişliğe bağlı kanallar) · aynı gün **5. tur** (kullanıcının iki kararı: OP-K5 = (a), K-B1 =
  (i); iki dallı yapı tek dala indi, kayıt telafi yolları istisna listesine girdi) · aynı gün **6. tur**
  (dar kapanış güvenlik okumasının RED'i: bir bloklayan, beş bloklamayan — orkestratörün kararları:
  K-B1'in uygulaması olarak `employeeAdd` istisna listesine, liste sekiz rota; `security_hold`'un nötr
  cümlesi paylaşılır) · aynı gün **7. tur** (dar kapanış güvenlik okuması ONAY; üç düşük bulgu —
  nötr cümle paylaşımının pini, kadroya eklemenin lokasyon ön koşulu, metin ve atıf düzeltmeleri).
- **Bağlam:** [M10 Akış A](../plan/m10-platform.md) §3, tasarım özünün *"Askı (ADR 0025)"*
  maddesi ve §2'nin akışlar arası çakışma çözümü (*"Askı (OP-15)"*); görev **OP-15** (A2,
  *"Askıya alma / yeniden etkinleştirme"*). OP-16A (migration `00034`) bu ADR'den önce sevk edildi
  ve *"tenant'ı değiştiren ilk `op_*`"* yükü orada doğdu; bu ADR o yükü yeniden tanımlamaz,
  [ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md) *"OP-16 uygulama notu"*na atıf yapar ve
  OP-15'in ondan farklarını yazar.
- **İlgili:** [ADR 0020](0020-platform-operatoru-ayri-kimlik.md) §4 (rota ve host), §5 (audit,
  K6), §9 (M9-08 kriterleri) · [ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md) §1 tablosu,
  §2 v 7 (yazma yalnız `void`/`uuid`, durum kehaneti yok; B14), §2 vii (duvar saati), §3, §6,
  sınır 1 ve 15, *"OP-16 uygulama notu"* (md. 3 (b2) commit edilmiş okumaya bağ; LV1–LV10) ·
  [ADR 0004](0004-policy-motoru-modeli.md) (karar motoru — **değişmez**) ·
  [ADR 0017](0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md) §5 (yarım encode) ·
  [ADR 0022](0022-islemsel-eposta.md) §7, [ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md),
  [ADR 0024](0024-kullanici-yukledigi-gorsel.md) §6 (davet, adres değişikliği ve marka rotaları) ·
  [ADR 0002](0002-tenant-baglami-ve-rls.md) md.6 · CLAUDE.md §4.3, §4.5, §4.6, §5, §6, §9

**Okuma kılavuzu — işaretler.** *"Kullanıcı kararı (2026-10-08)"* OP-K5 ve K-B1'in cevaplarıdır.
*"Orkestratör kararı (önerilen)"* OP-15 kart taslağının K15-1…K15-13 önerileridir; otonomi kuralıyla
uygulanır ve raporlanır. *"Orkestratör kararı (2./3./4. tur)"* denetim bulguları üzerine verilen
kararlardır. Bu ADR'de dallı bir yapı ve bekleyen bir **kullanıcı kararı** yoktur; ertelenen tasarım
maddeleri *"Karar verilmedi"*dedir. **Sözcük notu:** bu ADR'de *"kayıt"* yalnız **mesai kaydıdır**
(`transactions`); işletmenin ürüne katıldığı herkese açık akış (`/signup`) *"üyelik (signup)"* diye
anılır.

## Tek cümlede

Operatör bir tenant'ı askıya aldığında o tenant'ın **panelinden yapılan her yazma** —
`ProtectWriting` zincirindeki kümenin tamamı, kapalı ve sekiz rotalık bir istisna listesi (giriş,
seçim, çıkış, kendi parolası ve **kaydın dört yolu**: kadroya yeni kişi eklemek, yeniden davet ve
manuel kaydın iki adımı) dışında — **403** alır, reddin sebebi tenant'a bir cümleyle söylenir ve
tenant'ın kendi `audit_log`'una bir ret satırı düşer; **tap, aktivasyon, parola sıfırlama, giriş,
okumalar, CSV dışa aktarımı ve kaydın dört yolu askıyı bilmez**: askıdaki bir tenant'ın tap'i
askısız bir tenant'ınkiyle **aynı kararı ve aynı trust'ı** alır ve kaydı yazılır; hem kadrodakilerin
hem askıda kadroya yeni girenlerin yazılamayan kaydı telafi edilebilir (§4.6), çünkü mesai kaydı
hukuki delildir ve askı bir sözleşme yaptırımıdır, delil toplamayı durdurma gerekçesi değildir.

## Neden bir ADR

CLAUDE.md §10: *"Şema, karar motoru veya güvenlik sınırı değiştiyse ADR yaz."* Askı üçünden
ikisine değer ve üçüncüsüne bilerek değmez:
- **Şema:** `tenants`'a operatörün yazdığı ve `tappa_app`'in yalnız okuduğu bir durum girer;
  operatör audit tür kümesi iki tür kazanır; encode rölesinin kapalı hata sözlüğü bir sözcük kazanır.
- **Güvenlik sınırı:** §4.5'in tek istisnası (`op_*`) tenant'ın satırını ve `audit_log`'unu
  bağlamsız yazan **ikinci** bir fonksiyon çiftini kazanır; ve müşteri panelinin yazma zincirine
  (`ProtectWriting`) bir halka eklenir — bir kimliğin **yapabildiğini**, kimliği değişmeden,
  başka bir tarafın kararıyla daraltan ilk halka.
- **Karar motoru: DEĞİŞMEZ** ve bu ADR'nin yükünün yarısı bunu kalıcı kılmaktır. Askıyı bir
  guardrail yapmak ([ADR 0004](0004-policy-motoru-modeli.md) §4–§5, [ADR 0007](0007-guardrail-sirasi-ve-guvenlik-uyarisi.md))
  tap kararını sözleşme durumuna bağlardı; §4.6 bunun tersini ister.

## Bağlam — bugün (ölçüldü, HEAD `3f55d16`)

Kaynak okuması bu ADR'nin yazarınındır (HEAD `3f55d16`, 2026-10-07). Veritabanı olguları iki salt-okur
ölçümden gelir ve ikisi de bu ADR'de yeniden koşulmadı: OP-15 kart taslağının planlayıcı ölçümü
(2026-10-07, dal ucu `8575d24`, goose 33, `BEGIN TRANSACTION READ ONLY … ROLLBACK`) ve
[ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md) *"OP-16 uygulama notu"* md. 10 (aynı gün,
goose 33 → 34). `00034`'ün değiştirdiği yetkiler kaynaktan okundu.

| Olgu | Kaynak / ölçüm |
|---|---|
| `tenants`'ta askı sütunu yok; tabloda tetikleyici yok; sütunlar `id, name, vat_number, business_type, structure, plan, timezone, created_at, price_per_employee_month, vat_verified, vat_checked_at` | ADR 0021 OP-16 notu md. 10 |
| `tappa_app` `tenants`'ta: **tablo düzeyi** `SELECT` (yeni sütunları kendiliğinden okur), `INSERT` yalnız sütun düzeyinde (`id, name, vat_number, business_type, structure, timezone, vat_verified, vat_checked_at`), `UPDATE` yalnız `name, business_type, timezone`, `DELETE` yok | `00001:57`, `00016:87-89`, `00017:327`, `00024:119-122` |
| `tappa_opdefiner` `tenants`'ta: `SELECT` `id, name, business_type, plan, timezone, created_at, price_per_employee_month` (00029, 00032) + `vat_number, vat_verified, vat_checked_at` (00034); `UPDATE` **yalnız** `vat_verified, vat_checked_at` (00034) | `00034:363-364`; ADR 0021 §1 tablosu |
| `tappa_opdefiner` `audit_log`'da: `INSERT (tenant_id, actor_id, action, target, detail, at)` — **zaman sütunu dahil**; `id`'de, `SELECT`/`UPDATE`/`DELETE`'te yok. Yani *"tenant'ın günlüğüne yazan tanımlayıcı"* yetkisi **zaten var** (OP-16A) | `00034:365`; ADR 0021 OP-16 notu md. 5 |
| `audit_log`: `at` `DEFAULT now()`, **tetikleyicisiz** (LV8); tek FK `tenant_id → tenants` RESTRICT; iki append-only tetikleyici; `tappa_app` tablo düzeyi `SELECT, INSERT` — yani tenant kendi günlüğüne **her biçimde** satır yazabilir | `00005:247-248`; ADR 0021 OP-16 notu md. 10 |
| `operator_audit_log`: tür CHECK'i **on beş** tür (00034); tenant-yazma şekli CHECK'i (`operator_audit_log_tenant_write_shape`) yalnız `tenant_vat_checked`'ı adlandırır ve satırın `detail`'ini tam olarak `{}` ister; `actor_shape`'in oturumlu kolu yeni oturumlu türleri değişmeden kabul eder | ADR 0021 OP-16 notu md. 4; planlayıcı ölçümü |
| Panel yazmaları: `mountWriting` **27 POST** kaydeder — 23 doğrudan, marka logosu iç grubunda 1, encode rölesi iç grubunda 3; yolların hepsi sabittir, yolda id yok. `AdminAuth.Mount` toplam **30 POST** (artı `/admin/login`, `/admin/login/choose`, `/admin/logout`). `ProtectWriting()`'i kullanan tek yer `mountWriting`'dir | `internal/handler/dashboard.go:152-265`; `internal/handler/adminlogin.go:478-553`, `:705` |
| Kaydın dört yolu panel yazmasıdır: kadroya yeni kişi eklemek (`employeeAdd`), yeniden davet (`employeeInvite`) ve manuel kaydın iki adımı (`manualEntryReview`, `manualEntryRecord`) `mountWriting`'dedir | `internal/handler/dashboard.go:161-162`, `:190-191` |
| Kadroya yeni kişi eklemenin tek yolu `employeeAdd`'dir: depodaki tek `INSERT INTO employees` `CreateEmployee`'dir ve tek çağıranı `staff.go`'dur; yeni kişi `invited` doğar ve ancak davetle tap'e başlar; davet yalnız **var olan** bir satır için verilir; manuel kayıt çalışanı `employees`'ten okur (satır yoksa 0 satır, `ErrUnknownEmployee`) ve lokasyonunu **profilden** alır | `db/queries/employees.sql:708`; `internal/domain/tenant/staff.go:446`; `internal/handler/employeeactions.go:173-177`, `:299-302`; `db/queries/transactions.sql:354-359`; `internal/domain/manual/manual.go:402-436` |
| Davet **tek kullanımlıktır** (`used_at IS NULL`, `cancelled_at IS NULL` ve `now() < expires_at` koşullu tek tüketim) ve **süreli**dir (varsayılan 7 gün, en çok 30 gün); yeni bir davet çalışanın **bekleyen** davetlerini iptal eder; davet bağlantısı yöneticinin ekranında gösterilir. Çalışanın tap oturumu çerezi **yalnız aktivasyonda** yazılır; ikinci bir cihazın aktivasyonu çalışanın **öteki bütün oturumlarını** iptal eder ve bir audit satırı (`sessions_revoked` sayısıyla) yazar | `db/queries/invites.sql:223-232`; `internal/invite/manager.go:125-127`, `:225-232`; `internal/handler/employeeactions.go:395-398`, `:415-422`, `:451`; `internal/handler/activate.go:452-458`, `:476-486`, `:515` |
| Manuel kaydın geriye tarih sınırı **90 gün** (`MaxBackdate`; aşılırsa `ErrTooOld`); kayıt `channel='manual'`, `entered_by` dolu ve audit satırıyla aynı işlemde yazılır. Kayıt **onaysız** yazılır: `verdict` sabit `'ok'`, `queued` sabit `false` — onay kuyruğuna girmez, saat toplamına hemen girer, ve kişinin yön zincirine (son açık giriş) dahildir. **Düzeltme yalnız kısaltır:** erken yazılmış bir `out` ya da geç yazılmış bir `in` yeni bir kayıtla geri getirilemez; düzeltilen bir `in` kapatılamayan açık bir giriş bırakır (ADR 0011) | `internal/domain/manual/manual.go:87-145`, `:204`, `:452`; `internal/handler/manualentry.go:354`, `:480`; `db/queries/transactions.sql:16`, `:270-273`, `:356` (`practice`/`queued` sabitleri); CLAUDE.md §5; `TestManualDB_TheRecordAndItsAuditRowSHAREATransaction` |
| Parola sıfırlama (`/admin/reset`, panel dışı) **mevcut parolayı istemez** ve o yöneticinin **bütün** canlı oturumlarını iptal eder | `internal/adminauth/reset.go:609`; `db/queries/admins.sql:186-189` |
| Panel dışı POST'lar: `/activate`, `/api/activate` · parola sıfırlamanın iki adımı · `/api/checkin` · üyeliğin (signup) üç adımı · operatör yüzeyinin sekiz POST'u | `activate.go:145-148`; `adminreset.go:392-397`; `tap.go:226-231`; `signup.go:170-177`; `internal/handler/operator/routes.go:63-93` |
| **Yöntemsiz** rotalar: `/static/*` (`r.Handle`) ve yapılandırılmamış operatör yüzeyinde `/operator`, `/operator/*` (`r.Handle`, 503). chi v5.3.1 yöntemsiz bir rota tanımını **her yöntem** için uç nokta olarak yazar, yani rota yürüyüşü onları `POST` dahil her yöntemde raporlar | `internal/httpx/router.go:125`; `internal/handler/operator/surface.go:224-225`; chi `tree.go:363-373` |
| Üretim yönlendiricisi `run()`'ın içinde, canlı bağımlılıklarla satır içinde kurulur; paylaşılan bir kurucu yok. Deponun emsali gerçek yönlendiriciyi kurmak yerine kaynak taraması yapar | `cmd/tappa/main.go:722`; `internal/handler/admincookiepath_test.go:80-83` |
| Yöneticinin kendi parolasını değiştirmesi **aynı yöneticinin öteki bütün canlı oturumlarını** iptal eder (değişikliği yapan oturum kalır) | `internal/adminauth/changepassword.go:47-49`, `:109-116` |
| `TouchAdminSession` (`UPDATE admin_sessions … FROM admin_users`) panelin istek başına tek yetki ifadesidir ve bugün tenant hakkında hiçbir şey döndürmez; `tenants`'a bir join eklemek PK indeks taramasıdır | `db/queries/admins.sql:125-155`; planlayıcının `EXPLAIN`'i |
| Kimlik durumunun polarite kuralı: *"hiçbir şey yapmadan elde edilen durum zararsız olan olmalı"* — `AdminState`'in sıfır değeri `AdminUnresolved`'dır ve kimlik doğrulamaz | `internal/httpx/adminidentity.go:31-36` |
| Encode rölesinin hata gövdesi kapalı bir sözlüktür (sekiz sözcük). `refused`'un sunucudaki tanımı: *"the round failed on its own terms. The chip answered wrongly, the relay lied about a UID, or a gate in driver.go refused"*; bugün 422 ile döner | `internal/handler/plaqueencode.go:140-177`, `:151-152`, `:885-904` |
| **Android röle istemcisi** JSON gövdede önce `done`'u, sonra `fault` sözcüğünü okur; sözcük varsa durum kodunu kullanmaz. JSON olmayan bir gövde `Refused(status)` olur ve *"The server refused this sign-in. Sign in again."* der. `refused` için gösterilen cümle çipi suçlar ve *"Try a fresh plaque"* der; tekrar deneme kapalıdır. Bilinmeyen bir sözcük genel bir cümle alır (*"a fault this app does not know"*) ve tekrar denemeyi **açık** bırakır (`retryIsSafe` yalnız üç sözcüğü dışlar). Sunucu hatası turu **iptalsiz** bitirir; `Outcome.Faulted` bir `exchanges` sayısı taşır ve bu sayı **reddedilen değiş-tokuşu da içerir**: sayaç çip komutu işledikten sonra, yanıt sunucuya gönderilmeden önce artar, yani 3. değiş-tokuşun yanıtı reddedilince sonuç `Faulted(3)`'tür — sunucu 2 yanıtı kabul etmiş, çip 3 komutu işlemiştir | `android/…/relay/RelayServer.kt:79-108`; `Wording.kt:34-38`, `:46-47`, `:52`, `:119`; `RelayLoop.kt:21-41`, `:118-121`, `:163`; `RelayLoopTest.kt:122-127` |
| İstemcinin sözlüğü sunucununkine iki testle, sunucunun sözlüğü kendine bir testle bağlıdır: `ContractPinTest.theFaultVocabulary_matchesPlaqueencodeGo` `plaqueencode.go`'daki `fault… = "…"` sabitlerini okur ve `Wording.FAULT_WORDS` ile küme ve sayı olarak karşılaştırır; Kotlin `RelayLoopTest`'in her sözcüğü bir durum koduna eşleyen haritası `FAULT_WORDS` ile küme olarak eşit olmak zorundadır; Go'da `TestPlaqueEncode_WritesOnlyDeclaredFaults` sekiz sözcüğü **birebir** pinler | `android/app/src/test/…/ContractPinTest.kt:67-72`; `RelayLoopTest.kt:110-115`; `internal/handler/plaqueencode_test.go:910-913` |
| Bir encode turu 11 değiş-tokuştur (`internal/encode/driver.go:241-320`). İki kilometre taşı ADR 0017 §5.1'in adım numaralarıyla **aynı sayı değildir**: envanter satırı **4. değiş-tokuşun** (`getversion.3` — §5.1 adım 2'nin son çerçevesi, ardından **adım 3**) yanıtının sunucuda kabulünde yazılır; plaketin sırrı **9. değiş-tokuşta** (`changekey.sdmfileread` — §5.1 **adım 6**) çip komutu işlediğinde kurulur. ADR 0017 §5.3'ün kurtarma yolu **sevk edilmedi** | `Wording.kt:87-95`, `:133-136`; `internal/encode/driver.go:154-159`, `:253`, `:304`; backlog T77 |
| OP-16A'nın yazması önce tenant satırını birincil anahtarla `FOR NO KEY UPDATE` kilitler, **sonra** saati okur, sonra yazar | `db/migrations/00034_recheck_a_tenants_vat_from_the_operator.sql:889-897` |
| Müşteri tarafında `audit_log` okuyan iki sorgu: `ListPlaqueHistory` yalnız `action LIKE 'plaque.%'` döndürür (güvence sorgudadır); `ConfirmRecentRemoval`'ın eylemi bir **parametredir** (`action = @action`) ve aktörü oturumdaki yöneticidir — güvence onu çağıranların kapalı haritalarındadır: `venueRemovalWords` (`location.deleted`, `department.deleted`) ve `plaqueActWords` (üç `plaque.*` eylemi). İkisinden hiçbiri bir `tenant.*` satırı döndüremez | `db/queries/audit.sql:56-110`, `:216`; `internal/handler/locations.go:124`, `:1435`; `internal/handler/plaques.go:117`, `:686` |
| `opAtVersion` sonraki migration'ların Down'ını **test işleminin içinde** koşar; **10 çağrı noktası** (29, 30, 31×2, 32×3, 33×2, 34) | `internal/db/*_test.go` |
| Bir operatörün bugün bir tenant'ın yazmalarını durdurmasının ürün içi yolu yok; tek kaba araç sahibin psql ile yöneticileri `disabled` yapmasıdır — okumayı ve CSV'yi de keser, ve satır tenant'ın kendi eylemiyle ayırt edilemez | kaynak okuması |

## Karar

### Kullanıcı kararları (2026-10-08)

- **OP-K5 = (a) — askıdaki ay NORMAL faturalanır.** Soru: *"Askıya alınan bir işletme askıda geçen
  ay(lar) için faturalansın mı?"* Seçenekler (a) normal faturalanır · (b) askıda geçen günler orantılı
  düşülür · (c) ayın tamamı ya da herhangi bir günü askıdaysa o ay faturalanmaz · (d) operatörün ay
  başına takdiri. Gerekçe: askı sözleşmeyi değil yazma yetkisini durdurur; tap kaydı, okuma ve CSV
  sürer, yani hizmet sürer. Sonuç: fatura aritmetiği değişmez, OP-15 yalnız `tenants`'a iki sütun
  ekler, **askı geçmişi tablosu yoktur** (§1, §6; (b)/(c)/(d) ve geçmiş tablosu tasarımı *"Elenen
  seçenekler"*de).
- **K-B1 = (i) — kaydın telafi yolları askıda AÇIK.** Soru (4. tur, güvenlik okumasının bulgusu):
  askı, hiç yazılamayan bir kaydın iki telafi yolunu — yeniden daveti ve manuel kaydı — kapatıyordu;
  90 günü aşan bir askıda oturumu düşmüş bir çalışanın mesaisi kalıcı kayboluyordu (ölçüm §4). Karar:
  `employeeInvite` ve manuel kaydın iki adımı panel kapısının **kapalı istisna listesine** girer (§3).
  Gerekçe: bunlar işletmenin ayarı değil, §4.3/§4.6'nın telafi yoludur — askı delil toplamayı
  durdurmaz. Bedeli sayılı sınır 13'tedir; (ii) ve (iii) *"Elenen seçenekler"*de.
- **K-B1'in uygulaması: `employeeAdd` — orkestratör kararı (6. tur, 2026-10-08), kullanıcının
  ilkesinden türetildi, kullanıcıya bildirildi.** Güvenlik okuması (6. tur) kapılı kalan
  `employeeAdd`'in aynı kaybı **kadroya askıda yeni giren** kişi için ürettiğini ölçtü: kadroya kişi
  eklemenin tek yolu odur, davet ve manuel kayıt yalnız var olan bir satır için çalışır (Bağlam) —
  120 günlük bir askının 10. gününde işe başlayan kişinin 10.–29. günleri askı kalkınca 90 gün
  sınırının dışında kalırdı. Kadroya yeni giren kişinin kaydı da telafi yoludur; `employeeAdd`
  istisna listesine girer ve liste **sekiz** rota olur (§3). 27 panel yazmasının kayda etkisi §4'teki
  tabloda: yalnız bu dört rota, mesai kaydını ya da onun telafisini durduruyordu.

### 1. Temsil — `tenants`'ta iki sütun, kapının tek kaynağı

Askı `tenants`'ta iki sütundur ve **panel kapısının, operatörün okumalarının ve iki yazma
fonksiyonunun tek kaynağı** budur — orkestratör kararı (2. tur). Ayrı bir askı geçmişi tablosu yoktur
(OP-K5 = (a)); geçmiş iki append-only günlükte durur.

- `suspended_at timestamptz NULL` ve `suspension_reason text NULL`. İki CHECK: ikisi **birlikte**
  dolu ya da **birlikte** NULL; gerekçe kapalı kümededir. CHECK'ler sahibi de bağlar.
- **Gerekçe kümesi — orkestratör kararı (önerilen, K15-1):** `non_payment`, `terms_breach`,
  `security_hold`, `customer_request`. **Serbest metin YOK:** gerekçenin cümlesini tenant okur (şerit
  ve 403 sayfası; `security_hold`'un cümlesi nötrdür — §3, *"Gerekçe cümleleri"*) ve kod tenant'ın
  append-only `audit_log`'una girer; serbest metin oraya kişisel veri ya da operatörün iç notunu
  silinemez biçimde taşırdı. Küme şemada (CHECK), tanımlayıcının reddinde, Go'da, panelin cümle
  haritasında ve operatörün etiket haritasında durur — beş kopya; tek bir test onları birbirine bağlar
  (§2, *"Kopyalar"*).
- **Askı sütunları indekslenmez.** Bir indeks `UPDATE`'in HOT olup olmamasını **her sayfada** önceki
  duruma bağlardı (değişmeyen sütun HOT'a izin verir, değişen indeksli sütun vermez) ve `n_tup_hot_upd`
  önceki durumu söylerdi. İndekssiz hâlde bit kapanmaz, daralır: yalnız **neredeyse dolu bir sayfada**
  kalır (*"Sayılı sınırlar"* 4, genişliğe bağlı kanallar). Liste çipi (§5) indekssiz okur: liste zaten
  LIMIT'lidir.
- **`tappa_app` okur, yazamaz.** Tablo düzeyi `SELECT`'i iki sütunu kendiliğinden okur; `UPDATE`
  listesi (`name, business_type, timezone`) ve `INSERT` listesi değişmez — iki sütun hiçbirine
  girmez. **00024'ün ACL'i yeniden yazılmaz** — orkestratör kararı (önerilen, K15-2): yetki
  değişmiyor, ve yetkiyi son yazan dosyaya bağlamak 00016'nın disiplinidir. Bunun yerine A'nın ön
  koşulu **kayma denetimi** yapar (`00034`'ün ilk kez uyguladığı şekil, ADR 0021 OP-16 notu md. 8):
  `tappa_app`'in `tenants`'ta **tablo düzeyi** `UPDATE` ya da `INSERT`'i varsa migration reddeder —
  yeni sütun bir tablo düzeyi yetkiye kendiliğinden girer ve bir sütun yetkisi başka bir elin verdiği
  daha geniş bir yetkiyi daraltamaz (00024 ölçtü).
- **Tanımlayıcı** yalnız bu iki sütunda `SELECT` ve `UPDATE` kazanır. `audit_log` `INSERT`'i zaten
  vardır (`00034`); bu ADR ona sütun eklemez.
- **Geçmiş** iki append-only günlükte durur — operatör satırı ve tenant'ın kendi satırı (§2).

### 2. Operatör eylemleri — iki tanımlayıcı

İki fonksiyon, ADR 0021 §2'nin sözleşmesinin tamamıyla ve §6'nın katalog pinlerinin ileri ve ters
yönüne kendiliğinden girerek (her `op_*` için yeniden koşarlar):
`op_suspend_tenant(p_session, p_tenant_id, p_reason) RETURNS void` ve
`op_reinstate_tenant(p_session, p_tenant_id) RETURNS void`. Sahibi `tappa_opdefiner`,
`SET search_path = pg_catalog, pg_temp`, her nesne `public.`-nitelenmiş, `VOLATILE`, PUBLIC'ten
`REVOKE ALL`, yalnız `tappa_operator`'a `EXECUTE`; tanımı, sahibi ve yetkileri aynı migration'da
(§2 vi). Gövde çağıranın **tek işlemidir**.

**Gövdenin şekli (normatif; kesin metni A'nın).**
1. Oturum `op_touch_session` ile çözülür, aktör türetilir; ölü oturum → istisna, hiçbir yazı yok.
2. Argüman reddi **hiçbir tablo okunmadan** ve tek sabit mesajla (22023, DETAIL yok):
   `p_tenant_id` NULL; `p_reason` NULL ya da kapalı küme dışı.
3. **Önce kilit — orkestratör kararı (3. tur), OP-16A'nın sırası** (`00034:889-897`): tenant satırı
   **yalnız birincil anahtarla** okunur ve kilitlenir — `SELECT t.id FROM public.tenants AS t WHERE
   t.id = p_tenant_id FOR NO KEY UPDATE`. Okuma **hiçbir askı sütununu** döndürmez: gövde önceki
   durumu bir değişkene almaz. Tenant varsa satır bulunur (`FOUND`), önceki değerler ne olursa olsun;
   yoksa 0 satır — bekleme yok, kilit yok. `FOUND` yalnız tenant'ın varlığını söyler. `FOR NO KEY
   UPDATE` adım 4'ün `UPDATE`'inin zaten alacağı kilittir (anahtar sütunu değişmez): tenant'a satır
   ekleyen işlemlerin FK denetimi (`FOR KEY SHARE`) **beklemez** — tap kaydı dahil (OP-16A'nın LV5'te
   ölçtüğü sınıf).
4. **`IF FOUND` içinde, bu sırayla:**
   1. **Tek saat okuması** (`clock_timestamp()`), **kilit alındıktan sonra**; yazılan her zaman o
      okumadır.
   2. `tenants`'a yazan **tek** ifade, birincil anahtarla: `UPDATE … SET suspended_at =
      coalesce(suspended_at, <saat>), suspension_reason = p_reason WHERE id = p_tenant_id` (askıya
      alma) ya da `SET suspended_at = NULL, suspension_reason = NULL WHERE id = p_tenant_id` (yeniden
      etkinleştirme). `coalesce` satırın **kilit altındaki** değerini okur.
   3. Tenant'ın satırı: `INSERT … VALUES …` — `tenant_id = p_tenant_id`, `actor_id` = operatör,
      `action` `tenant.suspended` / `tenant.reinstated`, `target` tenant id'si, `detail` tam olarak
      `{"actor_kind": "operator", "reason": <kod>}` / `{"actor_kind": "operator"}`, `at` **aynı saat
      okuması, açıkça** (K6).

   Var olmayan tenant için bu bloğun hiçbir ifadesi koşmaz: FK hiç tetiklenmez, 23503 doğmaz, yani
   **varlık kehaneti yoktur** (B14).
   **Saatin yeri neden kilitten sonra — geriye tarihleme gerekçesinin yeni sırayla tutarlılığı.**
   ADR 0021 §2 vii'nin gerekçesi, bir zamanın **beklenen bir şeyden önce** okunup sonra yazılmasının
   satırı geriye tarihlemesidir (`audit_log.at`'in DEFAULT'u `now()` işlemin başında donar). Kilitten
   **önce** okunan bir saat aynı kusuru başka bir bekleyişle tekrarlardı: kilit bekleyen çağrı yazdığı
   zamanı bekleyiş kadar geriye tarihlerdi. Yarış, adıyla: X saati t1'de okur ve kilitten önce
   kesilir; Y saati t2'de okur, kilidi alır, askıya alır ve commit eder; X kilidi alır, yeniden
   etkinleştirir ve commit eder. Son durum askısızdır, ama yazılan zamanlar (X t1 < Y t2) iki günlükte
   askıyı **açık** gösterir — günlükleri okuyan biri (operatör, tenant, bir denetçi) var olmayan bir
   askı görürdü. Yeni sırada saat kilit altında okunur ve kilit commit'e dek tutulur, yani aynı
   tenant'a yapılan iki çağrının saatleri **işlemlerin uygulanma sırasıyla** okunur: X'in saati Y'nin
   commit'inden sonradır. `now()`'ın yasağı (§2 vii) ve kilit-sonra-saat sırası aynı kuralın iki
   yarısıdır: yazılan zaman, yazılan durumun geçerli olduğu andan **önce** olamaz.
   *Taslaktan fark (orkestratör kararı, önerilen):* kart `INSERT … SELECT … WHERE EXISTS` diyordu;
   OP-16A'nın 2. turu aynı yükü kilitli okuma + `IF FOUND … VALUES` ile kurdu (`EXISTS`'in READ
   COMMITTED'deki teorik yarışı kalktı — ADR 0021 OP-16 notu md. 11, F1). Burada da koşul kilitli
   okumanın `FOUND`'udur.
5. **Operatör satırı her kabul edilen çağrıda**, koşulun dışında: tür `tenant_suspended` /
   `tenant_reinstated`, oturum, aktör, `target_tenant_id = p_tenant_id`, `detail` `{}`.
   *K15-5 (orkestratör kararı, önerilen: `{}`)* artık bir tercih değil şemanın şeklidir: iki tür
   `operator_audit_log_tenant_write_shape`'in listesine iki ad olarak girer ve o CHECK `detail =
   '{}'` ister (`00034`). Operatör satırı neyin yazıldığını söylemez; onu tenant'ın kendi satırı söyler.
6. Kısıt yakalayıcı `00027` deseninde: tek 22023, DETAIL yok, LOG satırı yalnız kısıt adı ve
   SQLSTATE; alt işlemi adım 3–5'i birlikte geri alır.
7. **Dönüş ve hata, hedefin önceki durumundan bağımsızdır (§2 v 7).** Tenant yok, askısız ya da
   zaten askıda — aynı `void`. *"Zaten askıda"*, *"değişmedi"*, etkilenen satır sayısı yoktur. Sayılı
   tek istisna OP-16A'nın LV3'üdür: satırı **başka bir işlem** tutarken adım 3 bekler ve çağıranın
   kendi sınırları bekleyişi bir cevaba çevirebilir (`lock_timeout` altında 55P03) — bekleyiş yalnız
   tenant'ın varlığını söyler (*"Sayılı sınırlar"* 5).

**İdempotans — orkestratör kararları (önerilen).**
- **K15-4 — yeniden askı:** ilk `suspended_at` korunur (`coalesce`), gerekçe güncellenir; tenant
  satırı her çağrıda yazılır.
- **K15-11 — askısız tenant'ı yeniden etkinleştirmek:** tenant varsa tenant satırı her çağrıda
  yazılır — satır *"eylem uygulandı"* der, durumu okumaz. Gövde önceki durumu hiçbir adımda okumaz ve
  hiçbir yazının **var olup olmaması** önceki duruma bağlı değildir.

**Kemer (ADR 0021 §3.2).** Gövdede RLS yoktur; tek bariyer `p_tenant_id`'nin her ifadede açıkça
yazılmasıdır — kilitli okumanın `WHERE`'i, `UPDATE`'in `WHERE`'i, tenant satırının `tenant_id`'si ve
operatör satırının `target_tenant_id`'si. Kemer davranış testi bunları **ayrı** mutasyonlarla sınar: A
için yapılan çağrı B'yi değiştirmez ve B'nin hiçbir tablosuna yazmaz.

**Aynı işlem (K6).** Durum değişikliği, tenant satırı ve operatör satırı birlikte commit edilir ya da
hiçbiri. Rollback testi her yazı için bir kolla (emsal
`TestOpRecordVATCheck_TheThreeWritesShareOneTransaction`, `TestManualDB_TheRecordAndItsAuditRowSHAREATransaction`):
geri alınan işlem; sırayla her bir yazının yapılamadığı işlem — her kolda yazıların hiçbiri tek başına
kalmaz.

**Okuma.** `op_read_tenant_detail` dönüşünün **sonuna** iki askı sütununu alır; `CREATE OR REPLACE`
dönüş tipini değiştiremez → aynı dosyada `DROP` + `CREATE` + `ALTER OWNER` + `REVOKE`/`GRANT`
([m10-platform.md](../plan/m10-platform.md) → OP-11 A kart düzeltmesi md. 14.9; `op00029Functions`
pini HEAD'in imzasını değil `00029`'unkini ölçecek biçimde güncellenir). Orkestratör kararı
(önerilen, K15-3): `op_read_tenants` da aynı dosyada yalnız başlangıç zamanını alır (liste çipi; iki
okumanın `DROP`/`CREATE` dansı bir kez yapılır). Yeni sütunlar sona eklenir (Go konumla tarar).

**Kopyalar.** Audit tür kümesinin beş kopyası (tür CHECK'i, `op_begin_read`'in filtre listesi,
`op_read_audit`'in filtre şekli, Go'nun `OperatorAuditKinds`'ı, görüntüleyicinin `auditKindWords`'ü)
ve iki test kopyası (`opAuditKinds`, `opAuditKindsAddedBy`) **aynı commit'te** genişler — eksik
kalan `opAuditKindsAddedBy` eski sürüm testlerinin Down'da CHECK'i doğrulanmış geri almasını bozar.
`op_begin_read` ve `op_read_audit` `CREATE OR REPLACE` ile yalnız liste düzenlemesi alır; gövde
`00034`'ünkiyle aksi hâlde bayt bayt aynıdır, Down ona döner. Tür CHECK'i ve tenant-yazma CHECK'i
adı korunarak `DROP` + `ADD` ile zincirlenir; `NOT VALID` kuralı `00031`/`00033`/`00034`'teki gibi.
Gerekçe kümesinin beş kopyası (§1) tek testte birbirine bağlanır.

**Down — orkestratör kararı (önerilen, K15-6): askıdaki tenant varken REDDEDER.** `00034`'ün Down'ı
*"bir kapıyı kaldırır, bir olguyu değil"* (ADR 0021 OP-16 notu md. 7) — satırlar ve hükümler kalır.
Sütunları düşürmek her askıyı sessizce kaldırırdı: kapı değil olgu silinirdi. Bu yüzden Down askıdaki
bir tenant görünce reddeder; `opAtVersion` Down'dan önce askıyı test işleminin içinde kaldıran bir adım
kazanır ve askıya alan her E2E testi tenant'ı `t.Cleanup`'ta yeniden etkinleştirir. Down yetkileri
**adlı sütunlarla** geri alır (`REVOKE ALL` yok — `00029`'un ve `00034`'ün dersi).

**Ön koşul.** `00026`'nın rol denetimleri, `00034`'ün izleri (iki fonksiyonu tanımlayıcının, tür
CHECK'i `tenant_vat_checked`'ı adlandırır ve bu dosyanın türlerini adlandırmaz, tenant-yazma CHECK'i
var) ve kayma denetimi: tanımlayıcının `tenants` ve `audit_log` yetkileri tam olarak `00034`'ün
bıraktığıdır; `tappa_app`'in `tenants`'ta tablo düzeyi `UPDATE`/`INSERT`'i yok — üyelik kalıtımı
dahil (`TestOperator00034_PreconditionRefusesAWrongCluster`'ın D3 vakası emsal).

#### Commit edilmiş okumaya bağ — GEREKMEZ

OP-16A'nın yazması aynı oturumun aynı tenant için **commit edilmiş** bir okumasına bağlıdır (ADR 0021
OP-16 notu md. 3 (b2)) ve ADR 0021 *"Sonuçlar"* bunu sonraki tenant-yazan `op_*`'lara **emsal** sayar.
Askıda bu bağ **kurulmaz**, ve gerekçe bağın neden doğduğundan gelir:
- **Bağ bir sınamayı kapatıyordu.** `op_record_vat_check` çağıranın verdiği bir değeri
  (`p_vat_number`) saklanan bir değerle **karşılaştırır**; geri alınan savepoint'lerde tekrarlanan
  çağrılar *"bu tenant'ın kendi numarası mı"* bitini sayaçlardan izsiz okuyordu ve bit
  **yinelenebilirdi** — 8 haneli bir alan, aday başına ~474 µs (ADR 0021 LV2, 3. tur D1).
- **Askı hiçbir şeyi sınamaz.** İki gövde çağıranın verdiği hiçbir değeri saklanan bir değerle
  karşılaştırmaz: tenant id'si birincil anahtar araması için kullanılır, gerekçe kapalı bir
  kümeden gelir ve **yazılır, karşılaştırılmaz**, önceki durum hiçbir adımda okunmaz ve hiçbir yazının
  var olup olmamasını belirlemez (K15-4'ün `coalesce`'i yazılan değeri değiştirir, yazının kendisini
  değil). Geri alınan bir çağrının, **sayfada yer varken**, `pg_stat` sayaçlarından söyleyebildiği tek
  şey *"bu uuid bir tenant mı"*dır: B14'ün ve LV2 (ii)'nin bitidir, 122 bit rastgele bir uzayda
  yinelenemez, ve operatör id'leri zaten yalnız audit'li okumalardan öğrenir. **Satır genişliğine bağlı
  kanallar kalır, adıyla:** neredeyse dolu bir sayfada HOT sayacı, tablo dosyasının boyu ve WAL hacmi
  (*"Sayılı sınırlar"* 4) — önceki durumu söyleyebilirler, ama bir değer sınamazlar; A ölçer.
- **Ölçüt, sonraki tenant-yazan `op_*`'lar için:** gövde çağıranın verdiği bir değeri saklanan bir
  tenant değeriyle karşılaştırıyorsa (sınama), yazma o tenant'ın commit edilmiş bir okumasına bağlanır;
  karşılaştırmıyorsa bağ gerekmez. Bu ADR'nin iki fonksiyonu ikinci sınıftadır.
- **Bağın yan kazancı askıda yoktur, adıyla:** OP-16A'da bağ, satır kilidini **kasıtlı tutmayı**
  commit edilmiş bir okuma izine bağlamıştı (LV5, D2). Askının `UPDATE`'i de satırı kilitler ve
  bağ olmadan tutma **izsizdir** — *"Sayılı sınırlar"* 5; kalıcı çare rol düzeyi zaman aşımıdır
  (backlog T108), bağ değil.

### 3. Panel kapısı

**Yer: `ProtectWriting`'in son halkası.** Zincir: `floodGate → sameOriginGate → requireAdmin →
sessionGate → suspensionGate`. Kapı `ProtectWriting`'in **içindedir**, yani `ProtectWriting`'i
kullanan **her** rota — bugün `mountWriting`'in grubu, iç gruplar (marka logosu, encode rölesi) dahil;
istisna listesindeki rotalar hariç (aşağıda) — kapılıdır. EM-6'nın adres değişikliği ve
[ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md)/[ADR 0024](0024-kullanici-yukledigi-gorsel.md)'ün
renk, sıfırlama ve logo rotaları bu yolla kapsanır; hiçbirine rota başına kod eklenmez.
[ADR 0022](0022-islemsel-eposta.md) §7'nin davet rotası (`employeeInvite`) ve kadroya kişi ekleme
(`employeeAdd`) ise istisna listesindedir (K-B1 = (i) ve onun uygulaması).

**Neden `sessionGate`'ten sonra.** Kapı kimliğe ihtiyaç duyar (askı oturumun tenant'ınındır), yani
`requireAdmin`'den önce duramaz. Ve kapı bir **satır yazar**: `sessionGate`'ten önce dursaydı her ret
oturum bütçesini (300 / 10 dk) ödemeden append-only bir tabloya bir satır olurdu — kimlikli ama
sınırsız bir yazma ilkeli (`adminlogin.go`'nun *"an unbounded row here would be a write primitive"*
dersi). Sonra durduğu için satırları flood ve oturum bütçesi sınırlar. İç grupların kapıları
(`brandUploadGate`, `encodeGate`) bu halkadan **sonra** koşar: askıdaki bir tenant'ın logo yüklemesi
gövdesi okunmadan, kabul yeri ve bütçe harcanmadan reddedilir.

**Sıcak yol — ek sorgu yok.** Askı `TouchAdminSession`'ın kendi ifadesinden okunur: `FROM admin_users
a, tenants t … AND t.id = s.tenant_id`, `RETURNING` askının iki sütununu da döndürür →
`adminauth.Resolved` → `httpx.AdminIdentity`. RLS altında tenant satırı görünmezse join 0 satır verir,
oturum çözülmez — **fail-closed**. Kapı istek başında okur, önbellek tutmaz: askı bir sonraki istekte
etkilidir. Okuyucu askının satır kilidini beklemez (MVCC; B fazı ölçer).

**Polarite — orkestratör kararı (önerilen, K15-9), ve bu bir SAYILI İSTİSNADIR.** Kodun yazılı kuralı
`internal/httpx/adminidentity.go:31-36`'dadır: *"hiçbir şey yapmadan elde edilen durum zararsız olan
olmalı"* — `AdminState`'in sıfır değeri kimlik doğrulamaz. Go'daki askı alanı `Suspended`'dır ve sıfır
değeri *"askısız"*dır, yani **zararsız olmayan** yöndür: canlı bir kimliğin eşlemesinde askı düşerse
yazma **açılır**. İstisnanın kapsamı dardır — yalnız `AdminLive` bir kimlikte anlamlıdır, ve canlı bir
kimliği yalnız `Verify` üretir (sıfır değerli bir kimlik zaten canlı değildir). Gerekçe: ters polarite
(sıfır = kapalı) her handler sahtesini değiştirirdi. Karşısına konan **iki pin**: `Verify`'ın
eşlemesini askı sütunu düşerse kırmızıya dönen bir birim testi ve askı → aynı panel oturumunun sonraki
POST'u 403 diyen bir DB E2E'si (B fazında yazılacak; *"Güvenlik iddiası"* PART I).

**Kapsam — iki ayrı iddia.**
- **`ProtectWriting` grubuna eklenen her yeni rota kendiliğinden kapılıdır.** Bu yapıdır: halka
  zincirin içindedir.
- **Başka bir yere eklenen bir POST kendiliğinden kapılı DEĞİLDİR.** Onu yakalayan şey yapı değil
  testtir: rota yürüyüşü onu sınıfsız ya da kapısız bulur ve **kırmızıya** döner — ama yalnız
  yürünen yönlendirici üretimin kompozisyonundan **türüyorsa** (aşağıda, *"Hangi yönlendirici
  yürünür"*).
- Yürüyüş **bir liste değil bir türetmedir:** `AdminAuth.Mount`'un yönlendiricisi üzerinde GET/HEAD
  dışındaki her rota ya kapılıdır ya kapalı istisna listesindedir. `mountWriting`'in *"sayı yazılmaz"*
  kuralı sürer: kapsanan rota sayısı yoruma ya da bu ADR'nin kararına yazılmaz (*"Bağlam"* satırı bir
  ölçümdür, karar değil). Emsal yürüyüş `TestAdminRoutes_AreAllUnderTheCookiePath`'tir (`chi.Walk`).
- **Uygulamanın bütün rotaları da sınıflanır:** uygulama yönlendiricisinin yürüyüşünde **GET ve HEAD
  dışındaki her yöntem** (yalnız POST değil — chi yöntemsiz bir rota tanımını her yöntem için raporlar) şu
  kapalı sınıflardan birine düşer: (1) panel yazması (kapılı); (2) panelin adlı istisnası (aşağıdaki
  sekiz rota); (3) panel dışı adlı yüzey — tap, aktivasyon, parola sıfırlama, üyelik (signup), operatör
  yüzeyinin yapılandırılmış rotaları; (4) **yöntemsiz, yazmayan rotalar**, adıyla ve kapalı: `/static/*` (gömülü salt-okur dosya
  sunucusu) ve operatör yüzeyi yapılandırılmamışken `/operator`, `/operator/*` (her yönteme 503).
  Sınıfsız bir (yöntem, rota) çifti kırmızıdır. Testin ilk gün yeşil doğması bu dördüncü sınıfa
  bağlıdır (chi v5.3.1 `tree.go:363-373`).
- **Hangi yönlendirici yürünür — orkestratör kararı (4. tur).** Ölçüm: üretim yönlendiricisi
  `cmd/tappa/main.go:722`'de `run()`'ın içinde, canlı bağımlılıklarla **satır içinde** kurulur
  (`httpx.NewRouter(cfg, …, activation, tap, panelAuth, …)`); paylaşılan bir kurucu yoktur. Deponun
  emsali gerçek yönlendiriciyi kurmaktan **kaçınıp** kaynak taramasına geçmiştir
  (`internal/handler/admincookiepath_test.go:80-83`: Activation ve Tap'i kurmak veritabanı, davet,
  oturum, SUN doğrulayıcı ve audit ister). Panel düzeyindeki yürüyüş bundan etkilenmez —
  `AdminAuth.Mount` sahte bağımlılıklarla kurulur (`TestAdminRoutes_AreAllUnderTheCookiePath`). Karar:
  - B fazı `main.go`'nun ve testin **ortak çağırdığı tek bir yönlendirici kurucusu** yazar; test
    özellikleri sahte bağımlılıklarla verir, ama yönlendiricinin **kompozisyonu** (hangi özelliklerin
    hangi sırayla bağlandığı) üretimle aynı koddan gelir.
  - Bu yapılamazsa **yedek:** `NewRouter(...)` çağrısının argümanlarını kaynak taramasıyla sayan bir
    **alt sınır pini** — yeni bir özelliğin (`Mounter`) eklenmesi testi kırmızıya çevirir ve sınıflanmayı
    ister. Yedek pin var olan bir panel dışı özelliğin **içine** eklenen bir POST'u **yakalamaz**.
  - **Sınır, adıyla:** ortak kurucu yoksa *"başka bir yere eklenen POST'u yürüyüş yakalar"* iddiası
    **yalnız panel düzeyinde** (`AdminAuth.Mount`) geçerlidir; uygulama düzeyinde yalnız yedek pinin
    yakaladığı kadarıdır, gerisi kod incelemesinin konusudur.

**Kapalı istisna listesi — tam bu sekiz rota.**
- **Zincirin zaten dışında olan üç rota:** `/admin/login`, `/admin/login/choose`, `/admin/logout`.
- **`mountWriting`'in içinde, askı halkası olmayan ve adı konmuş bir zincirle duran kardeş grupta
  beş rota** — yapısal istisna, yol karşılaştırması değil; böylece
  `TestPanelProblemPages_CountTheWriteRoutesStillTellingReadersTheirPageIsEmpty`'nin `mountWriting`'den
  türettiği küme değişmez:
  - **yöneticinin kendi parolasını değiştirdiği rota** (`accountPasswordHref`) — gerekçe: meşru
    yönetici askıda da kendi kimlik bilgisini döndürebilmelidir; bu eylem işletmenin verisini
    değiştirmez ve aynı yöneticinin öteki oturumlarını düşürür (Bağlam; *"Sayılı sınırlar"* 11);
  - **kadroya yeni kişi eklemek** (`employeeAddHref`, `dashboard.go:161`) — K-B1'in uygulaması
    (orkestratör, 6. tur);
  - **yeniden davet** (`employeeInviteHref`, `dashboard.go:162`) — kullanıcı kararı (K-B1 = (i));
  - **manuel kaydın iki adımı** (`manualEntryHref`, `manualRecordHref`, `dashboard.go:190-191`) —
    kullanıcı kararı (K-B1 = (i)).

  Son dördünün gerekçesi: bunlar işletmenin **ayarı** değil, kaydın §4.3/§4.6'daki **yoludur**.
  Kadroya ekleme, askıda işe başlayan kişinin kaydının **ön koşuludur** (davet ve manuel kayıt yalnız
  var olan bir satır için çalışır); yeniden davet, oturumu düşmüş ya da yeni eklenmiş bir çalışanın
  tap'ini satır 3'ten çıkarır; manuel kayıt hiç yazılamamış bir mesaiyi (unutulan çıkış, oturumsuz
  geçen günler) kayda geçirir. Askı delil toplamayı durdurmaz (§4); bedeli *"Sayılı sınırlar"* 13'tedir.
- m10 §3'ün *"istisna parola değiştirme + çıkış"* cümlesi K-B1'den önceki listedir; güncellenmesi
  *"Sonuçlar"*da.

**Ret.** Askıdaki canlı bir kimlik kapılı bir rotaya geldiğinde:
1. Önce tenant'ın `audit_log`'una bir satır yazılır — `tappa_app` olarak, **tenant'ın kendi
   bağlamında** (§4.5'in istisnası değil): `action` `tenant.write_refused`, `actor_id` yönetici,
   `target` **rota deseni** (sabit bir küme; yolda id yok), `detail` tam olarak `{"reason": <kod>}`.
   Form değerleri, e-posta, ad satıra girmez.
2. Sonra **403** ve bir problem sayfası; gerekçe cümlesi kapalı bir haritadan gelir (aşağıda,
   *"Gerekçe cümleleri"*), haritada olmayan kod (sürüm kayması: yeni bir kod eski bir ikiliye ulaşırsa)
   genel bir cümle alır — boş bir cümle ya da ham kod değil.
3. Satır yazılamazsa da cevap **403**'tür ve bir `slog` Error satırı düşer: ret satırın yazılmasına
   bağlı değildir — fail-closed. Log satırının alanları **yalnız** tenant id'si, SQLSTATE ve kapalı
   kümeden gerekçe kodudur — form değeri, e-posta, ad, çerez ya da oturum değeri yok (B fazının
   yapılacağı; CLAUDE.md §7).
4. **Encode rölesinin üç yolu JSON ve YENİ bir sözcük alır: `suspended`** — orkestratör kararı (2. tur;
   K15-7'nin *"var olan `refused`"* önerisinin yerine geçer). Ölçüm (Bağlam): istemci sözcüğü durum
   kodundan önce okur ve sözcüğe göre konuşur; `refused` çipi suçlar, *"Try a fresh plaque"* der —
   askı bunların hiçbiri değildir ve tur ortasında gelen bir `refused` yarım yazılmış plaket için uyarı
   göstermez. JSON olmayan bir gövde (HTML 403) istemcide *"Sign in again"* olur — o da yanlıştır.
   Karar:
   - **Sunucu:** `plaqueencode.go`'da yeni bir hata sabiti (`suspended`) ve `encodeFaults`'a girdi;
     durum **403**; gövde `writeEncodeFault` ile yazılır (`TestPlaqueEncode_WritesOnlyDeclaredFaults`
     her çağrı noktasının bir sabit adlandırmasını ister). Sabit `plaqueencode.go`'da durmak
     **zorundadır**: istemcinin sözleşme testi sözlüğü o dosyadan okur. Biçim seçimi **rotadan**
     yapılır: rölenin üç yolu, başka hiçbir yol.
   - **Android:** `Wording.FAULT_WORDS`'e `suspended`; cümle askıyı adlandırır ve **çipi suçlamaz**;
     **"bu plaketi ayrı tut"** uyarısı taşır ve uyarı turun nerede kesildiğine göre kademelenir —
     plaket stoğu kısıtlıdır, boş bir plaketi boşuna ayırmak bir kayıptır. **Sayım kuralı (kaynaktan
     doğrulandı):** kapının reddettiği bir turda `Faulted.exchanges = n` reddedilen değiş-tokuşu da
     sayar — çip **n** komutu işlemiş, sunucu **n − 1** yanıtı kabul etmiştir (`RelayLoop.kt:118-121`;
     `RelayLoopTest.kt:122-127`'de 3. değiş-tokuşta `Faulted(3)`). Satır sunucu tarafı bir etkidir
     (yanıtın kabulünde yazılır), sır çip tarafı bir etkidir (komut işlenince kurulur); bu yüzden iki
     taş **farklı sayıya** uygulanır:

     | Koşul | Plakette kalan | Uyarı |
     |---|---|---|
     | `exchanges − 1 < 4` (`ROW_WRITTEN_AFTER_EXCHANGES`) | hiçbir şey; satır yok | plaket boş, askı kalkınca yeniden kullanılır |
     | `exchanges − 1 ≥ 4` ve `exchanges < 9` | envanter satırı var, sır kurulmamış | ayrı tut, sunucuyu işletene söyle |
     | `exchanges ≥ 9` (`SECRET_INSTALLED_AFTER_EXCHANGES`) | sır kurulu — yarım yazılmış | yeniden koşma, boş plaketlerden ayrı tut |

     Taşları doğrudan `exchanges`'e uygulamak `n = 4`'te — sunucu `getversion.3`'ün yanıtını hiç kabul
     etmemişken — boş bir plakete *"ayrı tut"* dedirtirdi. Değiş-tokuş numaraları ADR 0017 §5.1'in adım
     numaraları **değildir**: 4. değiş-tokuş `getversion.3`'tür (§5.1 adım 2'nin son çerçevesi, kabulü
     adım 3'ü — satırı — yazar), 9. değiş-tokuş `changekey.sdmfileread`'dir (§5.1 adım 6). **Tekrar
     deneme kapalıdır** (`retryIsSafe` `suspended` için `false`) — askı sürerken yeniden koşum aynı
     kapıya çarpar.
   - **İptal yok:** red turun handler'ından **önceki** bir kapıdan gelir; istemcinin bir iptali de
     aynı kapıdan geçip reddedilirdi. Tur sunucuda 90 sn'lik TTL ile kendiliğinden biter
     (`encodeGate`'in *"LET IT DIE"* emsali); istemcinin hata grubu yorumu (`RelayLoop.kt:21-41`)
     `suspended`'ı *"turun önündeki bir kapı"* grubuna alır.
   - **Birlikte değişir — OP-15'in B fazında aynı değişiklikte, beş yer:** (1) sunucunun sabiti ve
     `encodeFaults`; (2) `TestPlaqueEncode_WritesOnlyDeclaredFaults`'ın **birebir** sözcük listesi
     (`plaqueencode_test.go:910-913` — sözcükleri pinler, yani yeni sözcüğü kendiliğinden kapsamaz,
     düzenlenir); (3) `Wording.kt` (`FAULT_WORDS`, cümle, kademeli uyarı, `retryIsSafe`); (4) Kotlin
     `RelayLoopTest`'in sözcük → durum haritası (`RelayLoopTest.kt:110-115`; `suspended` → 403) — harita
     `FAULT_WORDS` ile küme olarak eşit olmak zorundadır; (5) sözleşme testi — iki sözlük birlikte
     değişirse değişmeden yeşil kalır, yalnız biri değişirse
     `ContractPinTest.theFaultVocabulary_matchesPlaqueencodeGo` kırmızıdır.
   - **Sürüm kayması — sevk sırası:** `suspended`'ı tanıyan APK, onu söyleyen sunucudan **önce**
     kurulur. Eski bir APK sözcüğü bilinmeyen sözcük sayar (`Wording.kt:46-47`): genel bir cümle
     gösterir, *"ayrı tut"* göstermez ve tekrar denemeyi **açık** bırakır (`retryIsSafe` yalnız üç
     sözcüğü dışlar). Sıra tutmazsa kalan kayma *"Sayılı sınırlar"* 12'dir.

**Gerekçe cümleleri — tenant'a ne söylenir (orkestratör kararı, 4. tur).** Şeritte ve 403
sayfasında gösterilen cümle kapalı bir haritadan gelir ve skill `tappa-brand`'in ses tonundadır:
suçlamasız, ne yapılacağını söyler. `non_payment` ve `terms_breach` kendi cümlelerini taşır — sebebi
bilmek tenant'ın onu gidermesini sağlar (ödemek, şarta uymak). **`security_hold`'un cümlesi
NÖTRDÜR:** sebebi adlandırmaz, yalnız işletmenin yazmalarının durdurulduğunu ve Taptime ile iletişime
geçilmesini söyler. Gerekçe: bu gerekçenin en olası sebebi ele geçmiş bir yönetici hesabıdır ve şerit
oturum sahibine — saldırgana da — gösterilir; *"güvenlik"* demek ona tespit edildiğini bildirirdi
(*"Sayılı sınırlar"* 11). Nötr cümle bildirimi azaltır, kaldırmaz: askının kendisi de bir işarettir.

**Nötr cümle PAYLAŞILIR — orkestratör kararı (6. tur).** Yalnız `security_hold`'a ait bir cümle B
fazında kaynak koda girer ve depo **herkese açıktır**: cümleyi kaynakta arayan biri gerekçeyi kesin
öğrenirdi. Bu yüzden tek bir nötr cümle **üç** girdinin ortak metnidir — `security_hold`,
`customer_request` ve haritada olmayan kod (sürüm kayması, ret 2) — ve haritada üçü **aynı sabite**
eşlenir; ekranda üçü aynı kelimeleri taşır, ayırt edilemez. Seçim ölçüldü: kapalı kümede (K15-1) bir
`other`/genel gerekçe **yoktur**; yalnız haritada olmayan kodun genel cümlesiyle paylaşmak yetmezdi,
çünkü o cümle pratikte yalnız sürüm kaymasında görünür ve askıda görülen her örneği `security_hold`
olurdu; `non_payment` ya da `terms_breach` ile paylaşmak o tenant'lardan gidermeleri gereken bilgiyi
alırdı. `customer_request`'in cümlesini nötrleştirmenin bedeli yoktur: askıyı isteyen tenant sebebini
bilir, ve bir sahibin kendi isteğiyle yaptırdığı askı (ör. işletmeyi kapatırken) ile Taptime'ın
güvenlik askısı dışarıdan aynı görünür. Panel ham kodu hiçbir yüzeyde render etmez (B fazının pini);
kod `tenants`'ta ve iki günlükte durur.

**Panel kabuğu.** Askıdaki tenant'ın her bölümünde bir şerit: başlangıç tarihi **tenant'ın saat
diliminde** (render katmanı; DB'de UTC), gerekçe cümlesiyle (yukarıda). Şerit ürünün kendi `Notice`
öğesidir ([ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md) §6 madde 2), tenant'ın accent'i onu
boyamaz; ayrıntısı skill `tappa-brand`. Orkestratör kararı (önerilen, K15-8): v1'de formlar
**gizlenmez** — şerit ve 403 sayfası yeter; formu gizlemek her bölümün şablonunu askıya bağlardı ve
kapının kendisinin yerine geçemezdi.

### 4. Dokunulmayanlar — §4.6 ve §5

Askı aşağıdakilerin **hiçbirine** girdi değildir:
- **Tap (NFC ve QR) ve karar motoru.** Ne `internal/domain/tap`, ne `internal/policy`, ne checkin'in
  bağlam toplayıcısı askıyı okur. Guardrail eklenmez; [ADR 0004](0004-policy-motoru-modeli.md) ve
  [ADR 0007](0007-guardrail-sirasi-ve-guvenlik-uyarisi.md) değişmez. **Ölçüt:** askıdaki bir tenant'ta
  §5'in her satırı NFC ve QR için askısız bir tenant'takiyle **birebir aynı** kararı, trust'ı, yönü,
  notu, kural kimliğini, practice bayrağını ve yazılan kayıt sayısını verir (gerçek Postgres; emsal
  harness `TestQRDB_PlaqueURLWithNoSUNBecomesARecordedQRTap`,
  `TestCheckinDB_ConfiguredFreshnessWindowReachesTheGuardrail`).
- **Aktivasyon** (`/activate`, `/api/activate`). Askıdan önce verilmiş, henüz kullanılmamış ve süresi
  dolmamış bir davet kodu askıda da kullanılır. **Telefonu sıfırlanan bir çalışan tap'e devam
  edebilir:** daveti tek kullanımlık olduğu için zaten harcanmıştır (`invites.sql:223-232`) ve tap
  oturumu çerezi yalnız aktivasyonda yazılır (`activate.go:515`), yani ona **yeni** bir davet gerekir
  — yeniden davet askıda açıktır (§3 istisna listesi; K-B1 = (i)). Yeni davet ikinci cihaz yolundan
  geçer: eski oturumlar iptal edilir, yenisi verilir ve bir audit satırı yazılır (`activate.go:452-458`,
  `:476-486`).
- **Parola sıfırlama** (`/admin/reset` ve yeni parola adımı) ve **giriş/seçim/çıkış.** Askıdaki
  tenant'ın yöneticileri girip okuyabilmeli ve dışa aktarabilmelidir.
- **Okumalar ve CSV.** `AdminAuth.Mount`'un her GET'i askıda ve askısız kimlikle **aynı durum kodunu**
  verir; iki CSV 200'dür ve saat raporunun `report.exported` satırı askıda da yazılır (bu bir iz
  satırıdır, işletmenin verisinde bir değişiklik değil). Gerekçe: GDPR veri taşınabilirliği ve
  işverenin yasal kayıt yükümlülüğü askıda da sürer.
- **Kaydın dört yolu** — kadroya yeni kişi eklemek, yeniden davet ve manuel kaydın iki adımı (§3
  istisna listesi).
- **Tap ve sonuç ekranı** (CLAUDE.md §9): askı bu iki ekrana hiçbir şey eklemez.

**§4.6'nın sonucu, adıyla:** askıda yazılan kayıt kaybolmaz, **karar bekler**: onay kuyruğu (FLAGGED
kayıtlar) birikir — onay vermek bir panel yazmasıdır ve kapılıdır. Hiç yazılamamış kayıt ise
**telafi edilebilir**:

**Kaydın yolları — kullanıcı kararı (K-B1 = (i), 2026-10-08) ve uygulaması (`employeeAdd`, 6. tur).**
Ölçüm (HEAD `3f55d16`, kaynak),
kararın neden gerektiğini gösteren:
- **Yeniden davet** bir panel yazmasıdır (`internal/handler/dashboard.go:162`). Davet **tek
  kullanımlıktır** (`db/queries/invites.sql:223-232`: `used_at IS NULL` ve `now() < expires_at` koşullu
  tek tüketim) ve **süreli**dir (`internal/invite/manager.go:125-127`: varsayılan 7 gün, en çok 30 gün).
  Çalışanın tap oturumu çerezi **yalnız aktivasyonda** yazılır (`internal/handler/activate.go:515`).
  Telefonu değişen ya da çerezi silinen bir çalışanın tap'i §5 satır 3'e düşer — aktivasyon sayfası,
  **kayıt yok** — ve onu geri getiren tek yol yeni bir davettir.
- **Kadroya yeni kişi eklemek** bir panel yazmasıdır (`dashboard.go:161`) ve bunun tek yoludur
  (`employees.sql:708`, tek çağıran `staff.go:446`). Davet yalnız **var olan** bir satır için verilir
  (`employeeactions.go:173-177`) ve yeni kişi `invited` doğar (`:299-302`); manuel kayıt çalışanı
  `employees`'ten okur — satır yoksa 0 satır (`transactions.sql:357-359`; `manual.go:402-436`
  `ErrUnknownEmployee`).
- **Manuel kaydın iki adımı** panel yazmasıdır (`dashboard.go:190-191`), ve manuel kaydın geriye tarih
  sınırı **90 gündür** (`internal/domain/manual/manual.go:204` `MaxBackdate`; `:452` `ErrTooOld`;
  `internal/handler/manualentry.go:354`, `:480`).
- **Bu dört rota kapılı olsaydı:** 90 günü aşan bir askıda, oturumu düşmüş bir çalışanın o günlerdeki
  mesaisi ne tap'le yazılabilirdi (kayıt yok, satır 3) ne askı kalkınca manuel girilebilirdi
  (`ErrTooOld`) — **kalıcı kayıp** (§4.6, §4.3). Aynı kayıp **kadroya askıda yeni girene** de olurdu:
  120 günlük bir askının 10. gününde işe başlayan kişi kadroya eklenemez, davet edilemez, ona manuel
  kayıt girilemez; askı kalkınca 10.–29. günleri 90 gün sınırının dışında kalır. Daha kısa bir askıda
  bile telafi yöneticinin hatırlamasına kalırdı.

**Karar:** dört rota kapalı istisna listesindedir (§3). Askıda yönetici işe başlayanı kadroya ekler,
kaybolan bir oturumu yeniden davetle geri getirir ve kaçan mesaiyi elle girer — hepsi kaydın yoludur,
işletmenin yapılandırmasının değişmesi değil. Unutulan çıkışlar da askıda elle kapatılabilir; saat
toplamları açık kayıtları saymadığını söylemeye devam eder. *"Kayıt kaybolmaz"* iddiası böylece hem
kadrodakiler hem askıda kadroya yeni girenler için tutar. Bedeli *"Sayılı sınırlar"* 13; (ii) ve (iii)
*"Elenen seçenekler"*de.

**27 panel yazmasının kayda etkisi** (6. tur güvenlik okumasının taraması, `dashboard.go:152-265`):
yalnız istisna listesine giren dört rota, mesai kaydını ya da onun telafisini durduruyordu; kalan 23'ü
durdurmuyor.

| Rota(lar) | Askıda kapalı kalınca kayda etkisi |
|---|---|
| `employeeAdd`, `employeeInvite`, `manualEntry` ×2 | **durduruyordu** → istisna listesinde (K-B1) |
| `review` (onay kararı) | kayıt yazılmaya devam eder; FLAGGED kayıt kuyrukta **karar bekler**, kaybolmaz |
| `employeeDeactivate` | kayıt yazılmaya devam eder (deaktive edilemeyen çalışanın tap'i de kaydedilir — sınır 11'in bedeli) |
| `employeeMove` | tap tap edilen lokasyona yazılır; manuel kayıt lokasyonu profilden alır (`transactions.sql:354-357`) — profil eski kalır, kayıt yazılır |
| `employeeEmail` | kayıtla ilgisi yok (davet bağlantısı yöneticinin ekranında gösterilir) |
| lokasyon/departman kaydet ve kaldır (×4) | kayıt mevcut ayarlarla yazılmaya devam eder (kanıt yoksa `flag`); telafi yolu manuel kayıt. **Ön koşul, adıyla:** kadroya ekleme var olan bir lokasyon ister (`employees.sql:713-719`); üyelik (signup) en az bir lokasyon yaratır (`internal/handler/signup.go:371`; kural `internal/domain/signup/signup.go:393`) ve lokasyon yalnız ona hiçbir şey bağlı değilken silinebilir (`locations.sql:340-366`, FK RESTRICT) — yani askıdaki tenant'ın her zaman en az bir lokasyonu vardır ve *"kadroya yeni girenler için de tutar"* iddiası onunla tutar. Askıda **yeni şube açılamaz**: o şubede çalışanın kaydı yazılır ama lokasyonu profilden gelir (`employeeMove` satırının kabulü); doğru şubeyi yalnız kaydın `note`'u taşıyabilir |
| plaket tak/değiştir/sök (×3) | takılı plaketlerde kayıt sürer; yeni bir girişte plaket yoksa telafi yolu manuel kayıt |
| encode rölesi (×3) | yeni plaket kişiselleştirilemez; telafi yolu manuel kayıt |
| kural değişikliği (×2) | kayıt mevcut kurallarla yazılır |
| fatura kapatma/dondurma (×2) | kayıtla ilgisi yok (§6) |
| hesap ayarı (`accountHref`) | kayıtla ilgisi yok |
| kendi parolası | istisna listesinde (başka bir gerekçeyle, §3) |
| marka (renk, sıfırla, logo) | kayıtla ilgisi yok |

### 5. Operatör ekranı

- **Rotalar** konsol grubunda (aynı zincir: `floodGate → sameOriginGate(true) → requireOperator →
  sessionGate`): `POST /operator/tenants/{id}/suspend` ve `POST /operator/tenants/{id}/reinstate`.
  Yoldaki id `uuid.Parse` ile doğrulanır, bozuksa store'a gidilmez; gerekçe kapalı kümede değilse
  400, store'suz; gövde `maxFormBytes` ile sınırlı.
- **Onay — orkestratör kararı (önerilen, K15-10):** tek POST + bir onay kutusu. Kutu CSRF savunması
  değildir (o `sameOriginGate(true)` + `SameSite=Strict`'tir), yanlış tıka karşıdır.
- **`security_hold` uyarısı — orkestratör kararı (4. tur, C fazı).** Formda `security_hold` seçilince
  kapalı metinli bir uyarı görünür: askı bir **yazma dondurmasıdır, çevreleme değildir** — ele geçmiş
  hesabın okumaları, CSV'si ve oturumları sürer; hesap askıda da **kadroya kişi ekleyebilir, yeniden
  davet basabilir ve manuel kayıt girebilir** (kaydın açık yolları); manuel kayıt **onaysız** yazılır
  ve gerçek bir çalışanın saatini düşüren bir kayıt yeni bir kayıtla **geri alınamaz** (ADR 0011);
  meşru yöneticinin savunma yazmaları kapanır; şerit oturum sahibine bir askı olduğunu gösterir
  (*"Sayılı sınırlar"* 11 ve 13'ün özü). Uyarı etkili eylemleri adlandırır: hesabı kapatmak (bugün
  sahibin psql ile yöneticiyi `disabled` yapması); meşru yöneticinin **parola sıfırlaması** — mevcut
  parolayı istemez ve o yöneticinin **bütün** oturumlarını düşürür, yani saldırgan parolayı
  değiştirdiyse asıl araç budur; ya da meşru yöneticinin parolasını değiştirerek öteki oturumlarını
  düşürmesi. Metin kapalıdır (kullanıcı girdisi ya da tenant verisi taşımaz).
- **Sonuç okumada görünür (PRG):** başarı → **303 genel bakış**; yazma `void` döndüğü için *"zaten
  askıda"* gibi bir mesaj yoktur (§2 v 7). `ErrOperatorRefused` → 303 giriş. Öteki hata → 503 ve
  *"kaydedilmedi"* **denmez**: autocommit bir `Exec`'te istemci hatası commit'ten sonra da gelebilir;
  cümle *"teyit edilemedi, genel bakışı açın"*dır.
- **Genel bakış:** durum satırı (askıda: UTC tarih + gerekçe etiketi; değilse *Active*) ve duruma göre
  **tek** form. Gerekçe etiketi kapalı haritadan; bilinmeyen kod ham değeriyle ve *"unrecognised"* ile
  (audit görüntüleyicisinin emsali, `internal/handler/operator/audit.go`).
- **Liste çipi (K15-3):** askıdaki tenant'ın satırında bir çip; renkler marka eşlemesinden
  (`tappa-brand`).
- **Bütçe:** yazma bir istek birimidir (`sessionLimit`); PRG okuması bir okuma birimidir
  (`readLimit`).
- [ADR 0020](0020-platform-operatoru-ayri-kimlik.md) §9'un *"ekranda tenant adıyla"* kriteri genel
  bakışın `TenantScreen` başlığıyla zaten karşılanır.

### 6. Faturalama — askıdaki ay normal faturalanır (kullanıcı kararı, OP-K5 = (a), 2026-10-08)

Askı sözleşmeyi değil yazma yetkisini durdurur: tap'ler kaydedilir, okuma ve CSV açıktır, yani hizmet
sürer. **Fatura aritmetiği değişmez** (`00016`'nın fonksiyonları, tenant önizlemesi,
`op_read_tenant_billing` — üç kopya, OP-12'nin çapraz testiyle bağlı) ve OP-15 faturalamaya hiçbir şey
eklemez; askı geçmişi tablosu yoktur. **İndirim gerekiyorsa** OP-17 (plan/fiyat) ya da elle.

**Yan etki, adıyla:** ayı dondurmak (`billingFreezeHref`) bir panel yazmasıdır ve kapılıdır, yani
**askıda bir ay dondurulamaz**; bitmiş aylar yeniden etkinleştirmeden sonra dondurulur. Tenant hiç
dönmezse aylar açık kalır; OP-12'nin operatör ekranı onlar için canlı önizleme gösterir.

### 7. Yapılmayacaklar (v1)

- **Sert kilit** — oturum iptali ya da yöneticileri devre dışı bırakan bir `op_*`
  (`op_disable_tenant_admins`). Askı mevcut oturumları kapatmaz (*"Sayılı sınırlar"* 6, 11).
  Orkestratör kararı (önerilen, K15-12): ayrı kart.
- **Tap reddi** ya da tap'te herhangi bir askı davranışı (§4).
- **Serbest metin gerekçe** (§1).
- **E-posta bildirimi** — tenant askıyı şeritten ve 403 sayfasından öğrenir (K15-12: ayrı kart).
- **Formları gizlemek** (K15-8).
- **Tur ortasındaki bir encode'u tamamlatmak** (*"Elenen seçenekler"*).
- **Askı geçmişi tablosu** (OP-K5 = (a); *"Elenen seçenekler"*).

## Güvenlik iddiası — üç parça (iskelet; OP-15'in fazları ilerledikçe doldurulur)

- **Tehdit modeli:** Bu pinler kazara sapmaya karşıdır; bir pini bilerek atlatmak kod incelemesinin
  konusudur. Kapsam: kapının yerini, kapsamını ya da istisna listesini; iki fonksiyonun SQL'ini;
  tanımlayıcının ve `tappa_app`'in yetkilerini; audit tür, gerekçe ve röle hata kopyalarını; ve tap
  yolunun askıdan bağımsızlığını düşüren, gevşeten ya da yerinden oynatan bir düzenleme.
- **PART I — ölçüm ve test adı.** Bu ADR yazıldığında OP-15'in hiçbir testi yoktur; aşağıda yazılacak
  testler **tarifleriyle** durur, adları yazıldıkları fazda verilir ve bu bölüme o fazın notu ekler.
  - *Bugün ölçülen (HEAD `3f55d16`):* yukarıdaki *"Bağlam"* tablosu — kapının takılacağı zincirin tek
    kullanıcısı `mountWriting`'dir; `tappa_app`'in `tenants`'taki `UPDATE`'i üç sütundur
    (`TestTenants00024_TheAppMayUpdateThreeColumnsAndDeleteNothing`) ve VAT sütunlarıyla şartlar
    `UPDATE`'e kapalıdır (`TestAccountDB_TheAppRoleHoldsNoUpdateOnTheVATColumnsOrTheTerms`) — A fazı
    ikisinin kapalı listelerini katalogdan türetilir hâle getirir; tanımlayıcının `tenants` ve
    `audit_log` hücreleri tam sütun listeleriyle `TestOperator00026_PrivilegeMatrix`'tedir; manuel
    kayıt ve audit satırı aynı işlemdedir (`TestManualDB_TheRecordAndItsAuditRowSHAREATransaction`).
  - *A fazında yazılacak:* yetki — `tappa_app` `tenants`'ın açık üç sütunu dışında `UPDATE` tutmaz
    (katalogdan türetilir, askı sütunları dahil; doğrudan `UPDATE` 42501; `INSERT` de yok), tanımlayıcı
    tam sütun listeleriyle, `tappa_operator` hiçbir şey; durum bağımsızlığı (yok / askısız / askıda →
    aynı sonuç; tenant yoksa tenant satırı yok ve hiçbir tenant değişmez); kemer (her ifade için bir
    mutasyonla); aynı işlem (her yazı için bir rollback kolu); `at`'ın duvar saati (uykusu tek bir
    `DO`'nun **içinde**, `now()` ve `statement_timestamp()` mutasyonları kırmızı); **saatin kilitten
    sonra okunduğu** (iki oturum: biri satırı tutarken öteki bekler; bekleyen çağrının yazdığı zaman
    tutanın commit'inden sonradır ve yazılan zamanların sırası uygulanma sırasıdır — saati kilitten
    önceye alan mutasyon kırmızı); CHECK'lerin sahibi de bağlaması; gerekçe kümesinin beş kopyasının
    eşitliği; geçici tablo gölgesi (`tenants`, `audit_log`, `operator_audit_log`; tanımlayıcıya `GRANT`
    adımıyla); ölü oturumlar; istatistik kehanetinin kalan bitinin sayılı değerleri (LV2'nin kalıbı)
    ve genişliğe bağlı üç kanalın ölçümü — **dolu sayfa vakası dahil** (HOT sayacı, tablo boyu, WAL);
    Down/Up ve K15-6'nın reddi; kayma denetiminin vakaları.
  - *B fazında yazılacak:* panelin rota yürüyüşü (kapılı her rota askıda kimlikle 403 + tam 1 ret
    satırı + 0 store çağrısı; aynı rota askısız kimlikle 403 **vermez** — vakumsuzluk); **istisna
    listesinin pini** — kapalı küme tam olarak sekiz rotadır ve kardeş gruptaki beşi (kendi parolası,
    kadroya kişi ekleme, yeniden davet, manuel kaydın iki adımı) **adlarıyla** tutulur: listeden biri
    düşerse ya da listeye dokuzuncu bir rota girerse kırmızı; bu beş rota askıda kimlikle 403
    **vermez** ve kendi işini yapar (ekleme kadro satırını, yeniden davet daveti, manuel kayıt kaydı ve
    audit satırını yazar); sonraki manuel kayıt askıda eklenen kişi için de geçer; `main.go` ile ortak
    yönlendirici kurucusu ve onun yürüyüşünde GET/HEAD dışı her (yöntem, rota) çiftinin dört sınıfı —
    *"panelin adlı istisnası"* sınıfı bu sekiz rotayı içerir; kurucu yazılamazsa yedek: `NewRouter(...)`
    argümanlarının alt sınır pini (§3, *"Hangi yönlendirici yürünür"*); okuma eşitliği (her GET aynı
    durum; iki CSV 200); tap eşitlik tablosu (§4); askı → aynı oturumun sonraki POST'u 403, yeniden
    etkinleştirme → aynı POST geçer; kendi kimlik bilgisi askıda değişir, çıkış askıda çalışır; ret
    satırı yazılamadığında log satırının alanları (yalnız tenant id'si, SQLSTATE, kapalı kümeden
    gerekçe kodu); panelin hiçbir yüzeyde ham gerekçe kodunu göstermediği ve `security_hold` cümlesinin
    sebebi adlandırmadığı; **nötr cümlenin paylaşımı** (§3, *"Nötr cümle PAYLAŞILIR"*) — gerekçe
    haritasında `security_hold`, `customer_request` ve haritada olmayan kod **aynı sabite** çözülür ve
    kendine ait cümleyi **yalnız** `non_payment` ve `terms_breach` alır: bu üçünden birinin kendi
    cümlesine ayrılması ya da başka bir gerekçenin ortak sabite katılması kırmızı; röle: üç yolda JSON `suspended` ve 403, store'a dokunmadan; `Verify`
    eşlemesinin pini (K15-9); Android tarafında `suspended`'ın cümlesi, kapalı tekrar denemesi ve
    kademeli uyarının sınırları (`n = 4` boş, `n = 5` satırlı, `n = 8` / `n = 9` sırrın iki yanı — §3
    ret 4'ün sayım kuralıyla).
  - *C fazında yazılacak:* operatör E2E'si (giriş → genel bakış → askıya al → 303 → genel bakış
    askıyı gösterir; görüntüleyicide `tenant_suspended` satırı tenant adıyla; tenant'ın günlüğünde
    `tenant.suspended`, `actor_kind` operator; yeniden etkinleştirme simetrik; var olmayan id → 303 →
    genel bakış 404); `security_hold` seçilince formdaki kapalı metinli uyarı (kaydın açık yolları,
    manuel kaydın geri alınamazlığı ve parola sıfırlama dahil).
  - *Kapanışta (K15-13, rig varsa):* çapraz yüzey — operatör POST → panel 403 → yeniden etkinleştirme
    → panel geçer.
- **PART II — adlı pinler.** Bugün var olan pinler, iki grupta:
  - *OP-15'in yeni nesnelerini **kendiliğinden** kapsayanlar (düzenlenmeden):*
    `TestOperator00026_ForwardCatalogPin` (sahip, `proconfig`, `EXECUTE`, ilk argüman, `void`/`uuid`
    dönüş pini — katalogdan türetir), `TestOperator00026_ReverseCatalogPin`,
    `TestOperator00026_NoFrozenClock`, `TestAdminRoutes_AreAllUnderTheCookiePath` ve
    `TestPanelProblemPages_CountTheWriteRoutesStillTellingReadersTheirPageIsEmpty` (grup değişikliğinden
    sonra yeşil kalmalı), Android'in `ContractPinTest.theFaultVocabulary_matchesPlaqueencodeGo` (iki
    sözlük birlikte değiştiği sürece).
  - *OP-15'in **aynı commit'te düzenlediği** pinler (yeni değeri adıyla ister):*
    `TestOperator00026_PrivilegeMatrix` (A — `tenants` hücreleri), `TestOperatorSQL_OnlyBoundParameters`
    ve `TestOperatorAccessors_TheCustomerRoleCannotUseThem` (A — iki erişimci),
    `TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree` ve
    `TestOperatorAuditKinds_TheTypedConstantsAreTheList` (A — on yedi tür ve tenant-yazma listesi),
    `TestAuditWords_NameEveryKindAndNothingElse` ve
    `TestE2E_AuditWordsCoverEveryScopeAndSearchClassTheDatabaseReturns` (A — görüntüleyicinin
    sözcükleri), `TestPlaqueEncode_WritesOnlyDeclaredFaults` (B — sözcük listesini **birebir** pinler;
    her çağrı noktasının bir sabit olması kuralı aynen kalır) ve Kotlin `RelayLoopTest`'in sözcük → durum
    haritası (B), `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` ve
    `TestSurface_TheScreensOfLaterTasksAreNotMounted` (C).

  OP-15'in kendi kaynak pinleri (gövdenin şekli ve kilitli okumanın saatten önce gelmesi, kapının
  zincirdeki yeri, istisna grubunun yapısı ve sekiz rotalık listesi, karar motorunun askıyı
  adlandırmaması) yazıldıkları fazda adlandırılır.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

## Elenen seçenekler

| Seçenek | Neden elendi |
|---|---|
| **Askıyı bir guardrail yapmak** (`sys:*`) | Tap kararını sözleşme durumuna bağlar; §4.6 ve bu ADR'nin özü. Guardrail eklemek [ADR 0004](0004-policy-motoru-modeli.md)/[ADR 0007](0007-guardrail-sirasi-ve-guvenlik-uyarisi.md) değişikliği ister; panel yazmalarının hepsi policy motorundan geçmez, yani kapsam rota başına bir hatırlamaya kalırdı |
| **Yöneticileri `disabled` yapmak** (bugünün kaba aracı) | Okumayı, CSV'yi ve kendi kimlik bilgisini değiştirmeyi de keser; tenant'ın eylemi ile operatörün eylemi günlükte ayrılmaz; geri almak her hesabı ayrı ayrı açmayı ister |
| **Kapıyı handler başına koymak** | Yeni bir yazma rotası kapısız doğar; kapsam bir test değil bir hatırlama olur. `ProtectWriting` içindeki halka o gruba eklenen rotayı kendiliğinden kapsar (m10 §2) |
| **Kapıyı `sessionGate`'ten önce koymak** | Ret satırı oturum bütçesini ödemez — kimlikli ama sınırsız bir append-only yazma ilkeli (§3) |
| **Askıyı ayrı bir sorguyla okumak** | İstek başına ikinci bir okuma; `TouchAdminSession` zaten istek başına tek yetki ifadesidir |
| **Faturalamada (b)/(c)/(d)** — orantılı düşüş, askıdaki ayı faturalamamak, operatörün takdiri | **Kullanıcı (a)'yı seçti, 2026-10-08** (OP-K5): askı yazma yetkisini durdurur, hizmeti değil (§6). Üçü de bir askı geçmişi tablosu ve fatura aritmetiğinin üç kopyasında değişiklik isterdi (aşağıdaki paragraf) |
| **Kapıyı bir aralık ya da geçmiş tablosundan okumak** (taslağın (b) dalı) | `EXISTS` RLS'in gizlediği bir tabloda *"askı yok"* der — fail-open; `tenants`'a join fail-closed'dır. Ve aralık güncellemesi yazmayı önceki duruma bağlardı (aşağıdaki paragraf) |
| **K-B1 (ii) — telafi yolları kapılı kalır, manuel kaydın süre sınırı askıyı hesaba katar** | Kullanıcı (i)'yi seçti, 2026-10-08. (ii) manuel kayıt alanını askıya bağlardı (`MaxBackdate` bilerek sınırlıdır — yanlış yazılmış bir yıl değişmez tabloya girmesin diye, `manual.go`'nun yorumu) ve askı sürerken oturumu düşmüş çalışanın tap'leri yine yazılamazdı; telafi yöneticinin hatırlamasına kalırdı |
| **K-B1 (iii) — askıya 90 günün belirgin altında bir üst sınır** | Kullanıcı (i)'yi seçti, 2026-10-08. (iii) bir ödeme askısını kendiliğinden kaldırırdı ya da operatöre süreli bir yenileme işi doğururdu, kapıya bir zaman karşılaştırması eklerdi; ve askı sürerken oturumu düşmüş çalışanın tap'leri yine yazılamazdı |
| **İstisnayı yol karşılaştırmasıyla kurmak** | Rota yeniden adlandırılınca sessizce kapanır ya da açılır; kardeş grup yapıyı görünür kılar (§3) |
| **`tappa_app`'e askı sütununda `UPDATE`** | Müşteri kendi askısını kaldırırdı; §4.5'in istisnası operatörün `op_*`'larıdır |
| **Serbest metin gerekçe** | Tenant okur ve append-only günlüğe girer — kişisel veri ve iç not silinemez biçimde (§1) |
| **Yazmanın durum döndürmesi** (*"zaten askıda"*) | §2 v 7: izsiz durum kehaneti; sonuç PRG okumasında görünür |
| **Yazmayı commit edilmiş okumaya bağlamak** | Askı bir değer sınamaz; bağın kapattığı sınıf burada yok (§2) |
| **Tenant satırını `WHERE EXISTS` ile koşullamak** (kart taslağı) | B14'ü karşılar; ama OP-16A'nın 2. turu aynı yükü kilitli okuma + `IF FOUND` ile kurdu; koşulu kilitli okumaya bağlamak *"değişti"* ile *"satır yazıldı"*yı tek olgudan türetir (§2) |
| **Saati kilitten önce okumak** (2. turun sırası) | Kilit bekleyen çağrı yazdığı zamanı bekleyiş kadar geriye tarihler; iki eşzamanlı çağrıda yazılan zaman sırası uygulanma sırasının tersi olabilir ve son durum askısızken günlükler askıyı açık gösterir (§2 adım 4'ün yarışı). OP-16A önce kilitler (`00034:889-897`) |
| **Down'ın askıları sessizce düşürmesi** | Down bir kapıyı kaldırır, bir olguyu değil (ADR 0021 OP-16 notu md. 7) — K15-6 |
| **Röleye var olan `refused` sözcüğünü vermek** (K15-7'nin ilk önerisi) | Ölçüldü (Bağlam): istemci sözcüğe göre konuşur; `refused` çipi suçlar, taze plaket ister ve yarım yazılmış plaket için uyarmaz. Askı turun değil işletmenin durumudur (§3 ret 4) |
| **`security_hold`'u tenant'a adıyla söylemek** | Şerit ve 403 sayfası oturum sahibine — ele geçmiş bir hesapta saldırgana — görünür; sebebi adlandırmak ona tespit edildiğini bildirirdi (§3, *"Gerekçe cümleleri"*; *"Sayılı sınırlar"* 11) |
| **Röleye HTML 403 vermek** | İstemci JSON olmayan gövdeyi oturum reddi sayar ve *"Sign in again"* der (`Wording.kt:52`) |
| **Tur ortasındaki bir encode'u askıya rağmen tamamlatmak** | Kapı her isteği okur; açık bir turu tanımak kapıya bellekteki tur durumunu okutmayı ister — kapının girdisi kimlik ve askıdır, encode deposunun durumu değil. (Global uid talebi — `tags`'in uid'si global birincil anahtardır, ADR 0017 §6 md. 12 — 4. değiş-tokuşun satır eklemesinde **zaten** yapılmıştır; tamamlanan turun eklediği kişiselleştirme ve `encoded_at` tenant içidir, yani gerekçe tenant-ötesi bir yazı değildir.) Yarım plaketin bedeli istemcinin uyarısıyla sınırlanır (§3 ret 4, *"Sayılı sınırlar"* 7). Bu bir ölçüm değil bir gerekçedir; tamamlatmanın daha güvenli olduğunu gösteren bir ölçüm kararı yeniden açar |

**Askı geçmişi tablosu — korunan tasarım (ileride fatura geçmişi gerekirse başlangıç noktası).** OP-K5
(b)/(c)/(d) için 2. turda tasarlandı ve OP-K5 = (a) ile doğmadı. Bir gün askı aralıklarını okuyan bir
tüketici (fatura, rapor) doğarsa şu kurallarla başlar:
- **Kaynak tenant'ın `audit_log`'u olamaz:** `tappa_app` oraya tablo düzeyi `INSERT` tutar (Bağlam),
  yani tenant kendi günlüğüne `tenant.reinstated` biçimli bir satır yazıp faturasını düşürebilirdi;
  kaynak yalnız tanımlayıcının yazabildiği bir tablo olmalıdır.
- **Olay satırları, aralık güncellemesi değil:** her kabul edilen ve tenant'ı bulan çağrı bir satır
  ekler — tenant, tür, gerekçe, zaman (§2 adım 4'ün kilit altındaki saat okuması). Satırlar eklenir,
  güncellenmez, silinmez (append-only tetikleyicileri `audit_log` ailesinin şekliyle). Açık aralık
  başına kısmi UNIQUE ya da `reinstated_at` güncellemesi **yoktur**: ikisi de yazmayı önceki duruma
  bağlardı (yeniden askı bir satır ekleyemez, askısız tenant'ı yeniden etkinleştirmek bir satırı
  güncelleyemez), sayaçlar *"şu an askıda mı"* bitini söylerdi ve `IF FOUND` tek koşul olamazdı.
  Aralıklar okunurken türetilir: bir yeniden etkinleştirmeden sonraki ilk askıya alma aralığı açar, bir
  askıdan sonraki ilk yeniden etkinleştirme kapatır; açık aralığın başlangıcı `tenants.suspended_at`'e
  eşittir (aynı saat okuması; saat kilitten sonra okunduğu için olayların sırası uygulanma sırasıdır).
- **Yazımı:** askının `UPDATE`'iyle aynı işlemde ve aynı `IF FOUND` koşuluyla; tenant yoksa `INSERT`
  yoktur (B14). Kemerin bir yeri daha olur (satırın `tenant_id`'si); satırın tür ve gerekçe CHECK'leri
  gerekçe kümesinin altıncı kopyasıdır.
- **Şema:** CLAUDE.md §6'nın beş öğesi; `tappa_app` yalnız `SELECT` (önce açık `REVOKE ALL`),
  tanımlayıcı yalnız `INSERT`, zaman sütunu açık duvar saatiyle; tenant tablosu, RLS izolasyon testi
  zorunlu. **Kapı onu okumaz** — kapının tek kaynağı `tenants`'ın iki sütunu kalır.
- **Down:** tablo fatura geçmişi taşıdığı için herhangi bir satır varken reddeder; `opAtVersion`
  `operator_audit_log` için bugün koşan işlem içi silme desenini (`internal/db/operatorplaques_test.go:139-166`)
  bu tabloya da uygular. Tablonun FK'si Down'ı *"Sayılı sınırlar"* 9'un kilit sınıfına sokar.
- **Tablo sonradan doğarsa** aradaki askıların aralıkları ondan okunamaz; o aralıklar iki append-only
  günlükte durur.

## Sayılı sınırlar

1. **Kapı uygulama düzeyindedir, veritabanı kilidi değildir.** `tappa_app`'in bütün yazma yetkileri
   askıda da durur: müşteri uygulamasının rolüyle koşan bir SQL enjeksiyonu ya da süreçte bir RCE askıyı
   aşar. Askı bir **ürün kontrolüdür**; veritabanı tarafında karşılığı yoktur ve olması tap'i de
   (aynı rol, aynı tablolar) etkileme riski taşırdı.
2. **TOCTOU penceresi.** Kapı istek başında okur; kapıdan geçmiş bir yazma, askı commit edildikten
   **sonra** commit olabilir. Pencere bir isteğin süresidir.
3. **DSN sahibi her tenant'ı askıya alıp kaldırabilir.** `tappa_operator`'ın DSN'ini tutan biri
   (ADR 0021 sınır 1) herhangi bir tenant'ı askıya alabilir ve kaldırabilir. İzi her çağrıda iki
   satırdır — operatör satırı ve tenant'ın kendi satırı — ikisi de append-only ve aynı işlemde (LV1'in
   sınıfı). Bir askıyı geri alınan bir işlemde **denemek** iz bırakmaz, ama etkisi de yoktur.
4. **İstatistik ve WAL kehaneti (ADR 0021 sınır 15, LV2'nin sınıfı).** Geri alınan bir savepoint'teki
   çağrı iz bırakmaz ama gözlenebilir şeyleri oynatır:
   - **`pg_stat` sayaçları:** `tenants`'ın birincil anahtar okuması (`idx_scan`/`idx_tup_fetch`) ve
     `n_tup_upd`'u ve `audit_log`'un `n_tup_ins`'i tenant varsa oynar, yoksa yalnız indeks taraması
     oynar — yani *"bu uuid bir tenant mı"*. Yazıların var olup olmaması önceki duruma bağlı olmadığı
     ve askı sütunları indekssiz olduğu için önceki durum hangi sayacın oynadığını değiştirmemelidir —
     **iddia, sayfada yeni satır sürümüne yer varken geçerlidir** (aşağıdaki kök); A ölçer ve sayılı
     değerleri pinler.
   - **Genişliğe bağlı kanallar — tek kök, adıyla.** `UPDATE` eski satır sürümünü sayfada bırakır ve
     yeni sürümü mümkünse aynı sayfaya yazar. Askıdaki bir tenant'ın satırı askı zamanı ve gerekçe
     kadar **daha geniştir**, yani önceki durum sayfadaki boş yeri ve eski sürümün baytlarını
     değiştirir. Bundan üç gözlenebilir şey çıkar, üçü de varsayılan izinlerle **herkese açıktır**:
     - **HOT sayacı** (`pg_stat_get_tuples_hot_updated`, `n_tup_hot_upd`): neredeyse dolu bir sayfada
       aynı çağrı bir durumda yeni sürümü sayfaya sığdırır (HOT), ötekinde sığdıramaz;
     - **tablo dosyasının boyu** (`pg_relation_size`): sığmayan sürüm yeni bir sayfaya gider ve tablo
       büyüyebilir;
     - **WAL hacmi** (`pg_current_wal_insert_lsn()`): aynı sayfada kalan bir `UPDATE`'in WAL girdisi yeni
       satırı eski satırla ortak baş ve son baytları atarak sıkıştırır; eski satırın askı sütunları
       (NULL mı, aynı zaman mı) ortak bayt sayısını, dolayısıyla girdinin boyunu değiştirir.
     Yani bu üçü **önceki durumu** söyleyebilir; eşzamanlı yük gürültü katar. **A fazının ölçümü bir
     dolu sayfa vakası içerir** (sayfayı yeni sürümün sığmayacağı kadar doldurup iki durumda aynı
     çağrı); kapatılmadı.
   - Öteki kanallar LV2'nin listesidir (blok sayaçları, zamanlama). Hiçbiri yinelenebilir bir sır
     değildir: tenant id'leri tahmin edilemez ve operatör onları da durumu da audit'li okumalardan
     zaten alır.
5. **Satır kilidini izsiz tutmak (backlog T108) ve bekleyiş.** Gövdenin argüman reddinden sonraki
   **ilk** ifadesi tenant'ın satırını birincil anahtarla `FOR NO KEY UPDATE` kilitler (§2 adım 3) ve
   kilit commit'e dek tutulur; işlemi açık bırakan bir DSN sahibi kilidi süresiz tutar ve geri
   aldığında iz kalmaz (§2'nin bağı yok). O sürece tenant'ın hesap ayarı yazması (`UpdateTenantAccount`), VAT
   yeniden denetimi ve öteki askı çağrıları **kilitli okumada** bekler; **tap'ler beklemez** (FK
   denetimi `FOR KEY SHARE`; LV5'te ölçülen sınıf) ve panel okumaları beklemez (MVCC). Bekleyen bir
   askı çağrısı saatini kilidi **aldıktan sonra** okur, yani bekleyiş yazılan zamanı geriye tarihlemez
   (§2 adım 4). Bekleyiş tenant'ın varlığını söyler — var olmayan tenant için kilitli okuma 0 satır
   döner ve beklemez (LV3'ün sınıfı; `lock_timeout` altında 55P03). Kalıcı çare rol düzeyi zaman
   aşımıdır — T108.
6. **Mevcut oturumlar kapanmaz.** Askı oturum iptal etmez; yöneticiler okumaya ve dışa aktarmaya
   devam eder. Etki bir sonraki istektedir (kapı önbellek tutmaz). Sert kilit ayrı karttır (§7).
7. **Yarım encode — ve kurtarma yolu SEVK EDİLMEDİ.** Askı bir encode turunun ortasına düşerse adım ve
   iptal reddedilir; tur bellekte TTL ile kendiliğinden biter (`encodeGate`'in *"LET IT DIE"*
   emsali, `TestPlaqueEncode_TheBudgetRefusesAndTheRoundDiesOnItsOwn`; düz plaket anahtarı süpürücüyle
   silinir — [ADR 0017](0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md) §6 md. 7). Plakette kalan,
   turun nerede kesildiğine bağlıdır (§3 ret 4'ün tablosu ve sayım kuralı; numaralar **değiş-tokuş**
   numarasıdır, ADR 0017 §5.1'in adım numarası değil): sunucu 4. değiş-tokuşun (`getversion.3`, §5.1
   adım 3) yanıtını kabul etmeden önce hiçbir şey yazılmamıştır, plaket boştur ve yeniden
   etkinleştirmeden sonra yeniden kullanılır; kabul ettikten sonra envanter satırı vardır (ADR 0017
   §5.2) ve `encoded_at`'sizdir — `00025` damgasız bir plaketin `active`'e geçişini her rol için
   reddeder; çip 9. değiş-tokuşun (`changekey.sdmfileread`, §5.1 adım 6) komutunu işledikten sonra
   plaketin sırrı kuruludur ve plaket yarım yazılmıştır. **ADR 0017 §5.3'ün kurtarma yolu tasarlandı,
   kodlanmadı** (backlog T77; `internal/encode/driver.go:154-159`; `Wording.kt:94`). O yol sevk edilene
   dek satırı olan bir plaket **ayrı tutulur**; istemcinin `suspended` uyarısı bunu söyler (§3 ret 4).
   🔴 **`tags` satırı hiçbir temizlikte SİLİNMEZ:** plaketin anahtarı yalnız o satırda (KEK ile sarmalı)
   durur ve çip anahtarını ya da dosya ayarlarını değiştirmek için **eski** anahtarı ister — satır
   silinirse çip kalıcı olarak kilitlenir (üretimde yanan tek çip tam böyle yandı — T77) ve
   kullanıcının plaket stoğu kısıtlıdır.
8. **Tanımlayıcı `audit_log.at`'i yazar (LV8).** `audit_log`'da `at`'i duvar saatine zorlayan bir
   tetikleyici yoktur; değer gövdenin tek saat okumasıdır ve davranış testiyle pinlenir (katalog bunu
   göremez). `tappa_app`'in tablo düzeyi `INSERT`'i de `at`'i kapsar. `00033`'ün operatör günlüğüne
   koyduğu tetikleyicinin `audit_log` karşılığı bir backlog adayıdır; bu ADR yazıldığında backlog'da
   onun maddesi yok (ölçüldü).
9. **Down ve kilit.** Down `tenants`'ta DDL yapar (`DROP COLUMN`, `DROP CONSTRAINT`) ve `opAtVersion`
   sonraki migration'ların Down'ını **test işleminin içinde** koşar (bugün 10 çağrı noktası):
   `tenants`'a dokunan paketler (fikstür INSERT'leri, FK denetimleri) o testin sonuna kadar bekler, ve o
   paketlerin kendi kilitleriyle bir döngü (40P01) ölçülmeden dışlanamaz. `00034`'ün Down'ı yalnız sütun
   `REVOKE`'u yaptığı için `tenants`'ta ve `audit_log`'da ilişki kilidi almıyordu (ADR 0021 OP-16 notu
   md. 7); OP-15'in Down'ı `tenants`'ta alır. OP-15 `audit_log`'da yetki değiştirmez (`00034`'ün
   `INSERT`'i yeter), yani oradan yeni bir kilit gelmez. A ölçer (paket testleri iki kez, 40P01/55P03
   aranır); Down'da hafif ifadeler önce, `tenants` DDL'i en son. Kalıcı çare — operatör tabloları
   danışma kilidini `tenants`'a dokunan paketlere genişletmek — ayrı bir karardır.
10. **Kapı kendi kimlik bilgisini değiştirmeye izin verir** (§3). Askıdaki bir tenant'ın ele geçmiş
    bir yöneticisi de kendi kimlik bilgisini değiştirebilir; işletmenin verisine dokunamaz. Hesabı
    kilitlemek sert kilidin işidir (§7).
11. **`security_hold` bir yazma dondurmasıdır, bir çevreleme değildir — neyi kapatmadığı, adıyla.**
    Ele geçmiş bir yönetici hesabında askı:
    - **saldırganı durdurmaz:** hesabın okumaları ve CSV dışa aktarımı sürer, oturumları açık kalır
      (sınır 6) — bu ADR'nin kendi kararı (§4), ve kapatmak sert kilidin işidir (§7);
    - **kaydın yollarını açık bırakır:** kadroya kişi ekleme, yeniden davet ve manuel kayıt istisna
      listesindedir (K-B1 = (i)) — saldırgan da kullanabilir; bedeli ve izleri sınır 13'te;
    - **meşru yöneticinin savunma yazmalarını da kapatır:** bir çalışanı devre dışı bırakamaz
      (`employeeDeactivateHref`, `dashboard.go:163`), çalınan ya da kaybolan bir plaketi
      değiştiremez ya da sökemez (`plaqueReplaceHref`, `plaqueUnmountHref`, `dashboard.go:182-183`) —
      kapı yazmanın amacını bilmez. Askı sürerken o çalışanın tap'leri ve o plaketin dokunuşları §5'e
      göre kaydedilmeye devam eder (§4.6);
    - **saldırgana tespit edildiğini bildirir:** şerit her bölümde, 403 sayfası her yazmada oturum
      sahibine görünür. `security_hold`'un cümlesi bu yüzden nötrdür (§3, *"Gerekçe cümleleri"*), ama
      nötr cümle bildirimi yalnız azaltır: yazmaların birden durması da bir işarettir, ve okumalar
      açık kaldığı için saldırgan bu işaretten sonra da dışa aktarabilir.
    **Askıda ürün içindeki iki çevreleme aracı**, ikisi de askının dokunmadığı yollardır:
    - **parola sıfırlama** (`/admin/reset`, panel dışı) — **mevcut parolayı istemez** ve o yöneticinin
      **bütün** canlı oturumlarını iptal eder (`internal/adminauth/reset.go:609`;
      `db/queries/admins.sql:186-189`). Saldırgan parolayı değiştirdiyse meşru yöneticinin **asıl**
      aracı budur;
    - **kendi parolasını değiştirme** (istisna listesinde) — **aynı yöneticinin öteki bütün canlı
      oturumlarını** iptal eder (`internal/adminauth/changepassword.go:47-49`, `:109-116`) ama mevcut
      parolayı ister.
    İkisi de iki yönlüdür (sınır 10): parolayı bilen saldırgan da önce davranıp meşru yöneticinin
    oturumlarını düşürebilir; askı bunu ne açar ne kapatır. Bu yüzden `security_hold` tek başına bir olay
    müdahalesi değildir: hesabı kapatmak bugün sahibin psql ile yöneticiyi `disabled` yapmasıdır
    (Bağlam), yarın sert kilittir (K15-12); operatörün formu `security_hold` seçilince bunu söyler (§5).
12. **Röle sözlüğünün sürüm kayması.** `suspended`'ı tanımayan eski bir APK, sunucu sözcüğü
    söylemeye başladıktan sonra da kullanılırsa sözcüğü bilinmeyen sözcük sayar (`Wording.kt:46-47`):
    genel bir cümle gösterir, *"ayrı tut"* uyarısını göstermez ve tekrar denemeyi açık bırakır. Tekrar
    deneme aynı kapıya çarpar (yazı yok), ama satırı olan bir plaketin ayrı tutulması gerektiğini
    kullanıcı öğrenmez. Çare sevk sırasıdır — önce APK, sonra sunucu (§3 ret 4); sıra tutmazsa kayma bu
    sınırdır.
13. **Kaydın yolları askıda açık — bedeli (K-B1 = (i) ve uygulaması).** Askıdaki bir tenant'ın her
    canlı yönetici oturumu — `security_hold`'da ele geçmiş bir hesap dahil — üç tür yazma yapabilir:
    - **Kadroya kişi ekleyebilir** (`employeeAdd`) — bir **hayalet çalışan** dahil — ve ona manuel
      kayıt girebilir (aşağıda). Ekleme bir audit satırı yazar (`employee.added`). Faturalama çalışan
      başınadır ve *"billable"* tanımı **aktivasyon** ister (`00016:239`, `p_activated_at IS NOT
      NULL`): askıda eklenen bir hayalet ancak **aktive edilirse** — yeniden davetle bir cihaz
      bağlanırsa — faturayı **büyütür** (askıdaki ay normal faturalanır, OP-K5 = (a)); yalnız manuel
      kaydı olan, hiç aktive edilmemiş bir hayalet **saat raporuna girer, faturaya girmez**. Ekleme
      **var olan bir lokasyon** ister (`employees.sql:713-719`) ve lokasyon kaydetmek kapılıdır: askıda
      yeni şube açılamaz; o şubede çalışanın kaydı yazılır ama **profil lokasyonuyla** (§4'ün
      tablosundaki `employeeMove` satırının kabulü).
    - **Yeniden davet basabilir** ve davet bağlantısı yöneticinin ekranında gösterilir
      (`employeeactions.go:395-398`); bağlantıyla **yeni bir cihaz bir çalışan olarak**
      etkinleştirilebilir. Bu ikinci cihaz yoludur: çalışanın **öteki bütün oturumları iptal edilir**
      (`activate.go:452-458`) — gerçek çalışanın telefonu tap'te §5 satır 3'e düşer — ve yeni cihazın
      tap'i **yine fiziksel kanıt ister**: NFC'de geçerli bir SUN dokunuşu (§5 satır 2), QR'da IP
      eşleşmesi (`base:qr-requires-ip`). İz: davetin kendisi ve aktivasyonun audit satırı
      (`sessions_revoked` sayısıyla, `activate.go:476-486`). **Telafi tek turluk değildir:** yeni bir
      davet çalışanın **bekleyen** davetlerini iptal eder — iptal eden ifade
      `CancelPendingInvitesForEmployee`'dir (`invites.sql:274`, `SET cancelled_at` `:319`), ve iptal
      edilen davet tüketilemez, çünkü tüketimin koşulu `cancelled_at IS NULL`'dır (`invites.sql:231`)
      (`employeeactions.go:415-422`, `:451`; `manager.go:225-232`) —, yani saldırganın oturumu yaşadıkça meşru yöneticinin
      gönderdiği bekleyen daveti iptal edip ele geçirmeyi **tekrarlayabilir**. QR'da IP eşleşmese de
      kayıt yazılır (`flag`, §4.6) ve onay kuyruğu kapılı olduğu için askıda **reddedilemez** — karar
      yeniden etkinleştirmeyi bekler.
    - **Manuel kayıt girebilir:** kayıt append-only'dir (§4.3) — `channel='manual'`, `entered_by` dolu,
      raporlarda ayrı görünür, `audit_log` satırıyla aynı işlemde
      (`TestManualDB_TheRecordAndItsAuditRowSHAREATransaction`). Ve **onaysız** yazılır: `verdict` sabit
      `'ok'`, `queued` sabit `false` (`transactions.sql:270-273`, `:356`) — onay kuyruğuna girmez, saat
      toplamına **hemen** girer, ve kişinin yön zincirine dahildir (`transactions.sql:16`
      `GetLastOpenTransaction`): sahte bir `in` gerçek çalışanın bir sonraki tap'ini `out` yapar.
      **Düzeltme yalnız kısaltır** (`manual.go:87-145` `CorrectionsOnlyShorten`;
      [ADR 0011](0011-duzeltme-satiri-yalnizca-kisaltir.md)): gerçek bir vardiyanın içine yazılmış
      sahte bir `out` yeni bir `out`la geri getirilemez, sahte bir `in` kapatılamayan açık bir giriş
      bırakır. Yani saldırgan **gerçek bir çalışanın saatini düşürebilir ve bu yeni bir kayıtla geri
      alınamaz**; parayı geri getiren tek yol bir telafi çiftidir (`in` + `out`), ve o da doğru olmayan
      bir ifade yazar (`manual.go`'nun yorumu).
    Hepsi iz bırakır ve **silinemez**; ama manuel kaydın etkisi yeni bir kayıtla tam tersine
    çevrilemez — yalnız kısaltılabilir ya da bir telafi çiftiyle dengelenebilir. Askı bu yazmaları ele
    geçmiş bir hesaba **açmaz** — askısız hâlde de açıktırlar; askı onları **kapatmaz**. Operatörün
    `security_hold` uyarısı bu yolları ve manuel kaydın geri alınamazlığını adlandırır (§5).

## Karar verilmedi

- **Sert kilit** (oturum iptali, `op_disable_tenant_admins`) ve **bildirim e-postası** — ayrı kartlar
  (K15-12); sınır 11 ve 13'ün çaresi.
- **Askı geçmişi tablosu** — yok (OP-K5 = (a)); bir tüketici (fatura, rapor) doğarsa *"Elenen
  seçenekler"*deki korunan tasarımdan başlanır.
- **Rol düzeyi zaman aşımı** (`tappa_operator`) — backlog T108; *"Sayılı sınırlar"* 5'i kapatır.
- **`audit_log.at` tetikleyicisi** — *"Sayılı sınırlar"* 8; backlog adayı.
- **Down kilidinin kalıcı çaresi** — *"Sayılı sınırlar"* 9.
- **Genişliğe bağlı kanalların kapatılması** (HOT sayacı, tablo boyu, WAL) — *"Sayılı sınırlar"* 4;
  A'nın ölçümünden sonra.
- **Formları gizlemek** — K15-8'in v2'si.

## Sonuçlar

- **OP-15'in A, B ve C fazları** bu ADR'yi normatif kaynak alır; OP-15 kartı (taslağı) bu ADR'ye
  atıfla `m10-platform.md`'ye taşınır. **A fazı başlayabilir.**
- **Bu ADR başka hiçbir belgeyi ve kodu değiştirmedi.** Aşağıdaki değişiklikler ilgili fazın
  commit'inde ya da orkestratörün belge işinde yapılır (OP-16B'nin ADR 0020/0021 eklemeleriyle
  çakışmamak için burada uygulanmadı):
  - **[ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md):** başlıktaki durum satırına OP-15 A
    ve C; **"OP-15 uygulama notu"** (A — bu ADR'nin üç parçalı iddiasının dolu hâli, ölçümler ve
    sayılı sınırları) ve **"OP-15 C fazı eki"**; §1 tablosunun `tappa_opdefiner` satırı — `tenants`'ta
    iki askı sütununda `SELECT` ve `UPDATE`; **§2 v 7'nin B14 cümlesine bir not:** normatif metin
    `INSERT … SELECT … WHERE EXISTS` biçimini adlandırıyor; *"tenant satırını birincil anahtarla
    kilitleyen okumanın `FOUND`'una koşullu `INSERT … VALUES` da aynı normu karşılar"* (OP-16A 2. tur
    ve bu ADR); **§2 vii'ye bir not:** kilit alan bir yazmada saat kilitten **sonra** okunur (OP-16A ve
    bu ADR — §2 adım 4'ün gerekçesi); *"Sonuçlar"*a bir OP-15 maddesi (bağın gerekmediği ve §2'deki
    ölçüt dahil); *"Karar verilmedi"*deki OP-K5 maddesinin kapanışı — cevap (a), 2026-10-08.
  - **[ADR 0020](0020-platform-operatoru-ayri-kimlik.md):** §4'ün rota listesine iki POST
    (`/operator/tenants/{id}/suspend`, `/operator/tenants/{id}/reinstate`); *"Karar verilmedi"*deki
    OP-K5 maddesinin kapanışı — cevap (a), 2026-10-08. *"Karar verilmedi"*nin `actor_kind` maddesi
    **değişmez**: OP-16A onu kapattı ve OP-15'in `tenant.suspended`/`tenant.reinstated`'ını adıyla
    kapsıyor; panelin `tenant.write_refused` satırı bir **yöneticinin** satırıdır, operatörün değil.
    İki müşteri okuyucusundan `ListPlaqueHistory` onu sorgunun kendi filtresiyle (`plaque.%`),
    `ConfirmRecentRemoval` çağıranlarının kapalı haritalarıyla (`venueRemovalWords`, `plaqueActWords`)
    döndüremez — maddenin atfı bu iki haritaya bağlanmalıdır.
  - **Android röle istemcisi ve sunucu sözlüğü (OP-15 B fazı, tek değişiklikte — §3 ret 4'ün beş
    yeri):** sunucuda `plaqueencode.go`'nun sabiti ve `encodeFaults`, ve
    `TestPlaqueEncode_WritesOnlyDeclaredFaults`'ın birebir sözcük listesi; Android'de `Wording.kt`
    (`FAULT_WORDS`'e `suspended`, askıyı adlandıran suçlamasız cümle, `exchanges − 1` / `exchanges`
    sayım kuralıyla kademeli *"bu plaketi ayrı tut"* uyarısı, `retryIsSafe`'te `suspended` için
    `false`), Kotlin `RelayLoopTest`'in sözcük → durum haritası (`suspended` → 403), `RelayLoop.kt`'nin
    hata grubu yorumu (`suspended` turun önündeki bir kapıdır, iptal yok); `ContractPinTest` değişmeden
    yeşil kalır çünkü iki sözlük birlikte değişir. **Sevk sırası:** önce APK, sonra sunucu (sınır 12).
    `android/README.md`'nin sözlük anlatımı da aynı değişiklikte — README bugün (`:103`) *"the seven
    fault words"* diyor, sözlük ise sekiz sözcüktür; sayı OP-15'ten bağımsız olarak zaten bayattır
    (bu ADR düzeltmez, listeler).
  - **[ADR 0004](0004-policy-motoru-modeli.md), [ADR 0007](0007-guardrail-sirasi-ve-guvenlik-uyarisi.md):
    değişmez.** **[ADR 0017](0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md): değişmez** (yarım
    encode bu ADR'nin sayılı sınırıdır; §5.3'ün sevki T77'dir). **[ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md):
    değişmez** (şerit `Notice`'tir, §6 madde 2).
  - **[ADR 0005](0005-kabul-edilen-riskler.md):** bu ADR'nin kabul edilen riskleri (*"Sayılı
    sınırlar"* 1–3, 11 ve 13) *"Append kuralı"*nın konusudur; ADR 0020 sınır 8'in açık işiyle (operatör
    risklerinin ADR 0005'e eklenmesi) birlikte orkestratörün kararı — eklenirse
    `TestADR0005_TheRiskCountMatchesTheTable` aynı değişiklikte güncellenir.
  - **`m10-platform.md`:** §3 *"Askı (ADR 0025)"* maddesinin istisna cümlesi (*"istisna parola
    değiştirme + çıkış"*) bu ADR'nin sekiz rotasına bağlanır; §6'ya OP-K5 = (a) ve K-B1 = (i), tarihli
    (kullanıcı kararları, 2026-10-08); OP-15 kartı.
  - **Backlog adayı:** `audit_log.at` tetikleyicisi (*"Sayılı sınırlar"* 8). T108 *"Sayılı sınırlar"*
    5'i de kapsar — maddesine askı eklenir. T77 *"Sayılı sınırlar"* 7'nin kurtarma yoludur.
  - **CLAUDE.md değişmez:** §4.5'in istisna cümlesi (`op_*`) askının iki fonksiyonunu kapsar; §5'in
    karar tablosu ve §9'un tap ekranı kuralı askıdan etkilenmez.
- **Devirler:** OP-18 — operatörün `plaque.*` satırları müşterinin plaket geçmişinde *"adsız
  yönetici"* okunur (ADR 0020 *"Karar verilmedi"*, ADR 0021 LV9); `plaqueActWords` `plaque.*`
  eylemlerini adlandırsa da `ConfirmRecentRemoval`'ın aktör filtresi oturumdaki yöneticidir, yani
  operatörün satırı oradan dönmez — OP-18 bu ikisini birlikte okumalıdır; OP-12 B devir 6
  (*"ekran askı bilgisi göstermez"*) aynen kalır — faturalama ekranı askıyı göstermez, genel bakış
  gösterir.
