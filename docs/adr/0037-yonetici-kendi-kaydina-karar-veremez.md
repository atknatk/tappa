# ADR 0037 — Yönetici kendi kaydına karar veremez: panel hesabını çalışan satırına bağlamak

- **Durum:** **Kabul edildi (2026-11-07).** Kullanıcı §9'daki üç soruda önerileri seçti; sahip muafiyeti bildirildi, itiraz yok. **T148-0 uygulandı; T148-1 ve sonrası yazılmadı.** Şart: **bağ ve engel, ilk saat basan yöneticiden önce çıkar** (§9 soru 1).
- **Tarih:** 2026-11-06 (kabul 2026-11-07) · **Backlog:** T148
- **İlgili:** ADR 0004 (Y-C) · ADR 0005 (Y-D) · ADR 0010 · ADR 0027 (K-1a) · ADR 0031 · ADR 0032 (G-O1) · ADR 0036 · CLAUDE.md §4.3, §4.5, §4.6, §5
- **Atıflar** `origin/m14-identity` `ffbf1808`'e göredir. **Ölçüldü** = geçici Postgres, goose 87, `tappa_app`, 2026-11-06. **Kod okundu** = çalıştırılmadı. **(incelemede)** = bulgu incelemeden geldi, atıfı doğrulandı, ölçülmedi.

## Özet

Bir yönetici bugün kendi işaretli kaydını onaylayabilir, kendi saat talebini onaylayabilir ve kendi saatini onaysız elle girebilir. Koruma üç yerde yazılı ama çalışmıyor: farklı tablolardan gelen iki id'yi karşılaştırıyor.

**Canlıda bugün yönetici yok.** Dört işletmenin dördünde bir sahip, sıfır yönetici var. Yani açık henüz kullanılamıyor, ama davet yüzeyi açık (`TAPPA_ADMIN_IDENTITY=on`) ve yönetici tek basışla doğar. Bu yüzden asıl soru *ne yapılacağı* kadar *ne zaman* yapılacağıdır; erteleme dürüst bir seçenektir.

**Öneri:**
1. Rules ekranındaki yanlış vaat düzeltildi (T148-0).
2. İlk saat basan yöneticiden önce, sahibin kurduğu bir bağ gelir: `employees.admin_user_id`.
3. Bağlı yönetici kendi kaydına karar veremez; kendi elle girişi işaretli yazılır ve başka yetkiliye düşer.
4. Bağlı sahip kendi kaydına karar verir, audit'e damgalanır.

Koruma yalnız **bağlı** hesapta çalışır. Bağ kurulmamışsa hiçbir şey değişmez.

## 1. Sorun

### 1.1 Koruma neden ölü

Kaydın sahibi `employees.id`, karar veren `admin_users.id`. Aynı kişide bile eşit değiller; aralarında sütun, FK ya da sorgu yok.

| Yer | Karşılaştırma | Bugün |
|---|---|---|
| `db/migrations/00005_create_transactions_audit_reviews.sql:354` | `NEW.reviewer_id = v_employee_id` | Tutmaz |
| `db/migrations/00068_create_punch_claims_and_their_decisions.sql:313` | `NEW.decided_by = v_employee` | Tutmaz (`:268-271` kendisi söyler) |
| `internal/domain/claim/decide.go:292` | `d.DecidedBy == c.EmployeeID` | Yalnız sahte satırla (`TestDecideDB_TheSelfDecisionCheckFiresOnlyOnAForgedIdentity`) |
| `internal/policy/guardrails.go:610-621` | `ReviewerID == SubjectEmployeeID` | Koşmaz; alanları yalnız bir sentetik yoklama doldurur (`internal/domain/tenant/rulebook.go:933`), panel motoru çağırmaz (`TestPanel_CallsThePolicyEngineNowhere`) |

Müşteriye görünen **Rules ekranı** T148-0'a kadar *"nobody approves their own record"* diyordu. LP-07 bunu public sayfalardan kaldırmış, panelde bırakmıştı. **M14 boşluğu kapatmadı:** ADR 0031 yalnız yönetici satırlarını bağlar (`:124`, *"Çalışan kimliği … etkilenmez"*).

### 1.2 Kim neyi yapabiliyor

| Yol | Rol kapısı | Yönetici kendi kaydına | Saate etkisi |
|---|---|---|---|
| İşaretli kayıt (`internal/handler/dashboard.go:186`) | Yok | **Ölçüldü: yazıldı** | Sayılır (`internal/domain/ledger/report.go:819-832`) |
| Saat talebi (`internal/handler/claimqueue.go:248`, `:56`) | Yok | Kod okundu: aynı boşluk | Manuel satır yazar (`decide.go:333-341`) |
| **Elle kayıt** (`dashboard.go:409`) | Yok | **Ölçüldü: `ok` yazıldı, onay yok** | Hemen sayılır (`db/queries/transactions.sql:473-489`) |
| Rapor düzeltmesi | Ayrı yol yok | Düzeltme yeni manuel satırdır (`internal/domain/manual/manual.go:89-112`) | Manuelle aynı |
| Dış API (`internal/handler/api/surface.go:192-197`) | Sahibin anahtarı | Saat yazan uç yok | Yok |

Yalnız onay yollarını kapatmak işe yaramaz: engellenen yönetici aynı saati elle yazar. Aynı vitrin argümanı §6'daki atlatma için de geçerlidir.

**Tehdit:** asıl tehdit ücreti ödeyen sahibe karşı kendi saatini şişiren yönetici. Sahibin kendi kaydında aldatılan biri yok.

### 1.3 Canlı sonuç (orkestratör ölçtü, 2026-11-06)

Sorgular `216e6533`'teki ADR §10'da; yalnız sayı döner.

| Ölçü | Sonuç |
|---|---|
| İşletme | 4 (Creekville'de kadro yok) |
| Sahip / yönetici | Her birinde 1 / 0 |
| Kadro | 2, 0, 8, 22 |
| Yetkiliyle aynı adresli çalışan / yalnız aynı adlı | 0 / 0 |
| Adrese göre öz-karar | 0 |
| `transaction_reviews` / `punch_claim_decisions` / manuel satır | 17 / 0 / 0 |
| Bekleyen işaretli / bunların yetkilinin kendi kaydı olanı | 3 / 0 |

KF ve KM'nin aynı kişiye ait olması bu ölçümden gelmez; kaynağı `db/migrations/00011_add_admin_resolution.sql:22-23`.

Sayılar çok küçük. **"0 öz-karar" davranış kanıtı değildir, yapısaldır:** yönetici yok, ve hiçbir yetkili adres ya da adla kadroda görünmüyor.

## 2. Piyasa ve sahip varsayılanı

Araştırma 2026-11-06.

| Ürün | Bulunan | Kaynağın gücü |
|---|---|---|
| [PayHero](https://support.payhero.co.nz/en/articles/11956580-timesheet-approval) | *"Team managers are not able to approve their own time"*; admin ya da üst yönetici onaylar | Sayfa okundu |
| [Tempo](https://help.tempo.io/kb/latest/can-i-approve-my-own-timesheet) | Onay izni olanlar *"cannot approve their own timesheets"*; başkası onaylar. Proje zaman çizelgesi ürünü | Sayfa okundu |
| [When I Work](https://help.wheniwork.com/articles/role-permissions-reference/) | Supervisor için kutucuk: *"Approve button for their own timesheet"*; üst roller için söz yok | Sayfa okundu |
| [Roubler](https://support.roubler.com/hc/en-us/articles/31309756452249-Prevention-of-Managers-Approving-own-Timesheets) | Varsayılan: yönetici kendi kaydını onaylayamaz; istisna izinle | 403, arama özeti |
| [Deputy](https://help.deputy.com/hc/en-au/articles/13841429485967-Approve-Timesheets-and-Export-Timesheets-Permissions) | Rol izni; izni yoksa [Approve yerine Update görür](https://help.deputy.com/hc/en-au/articles/4689553875471-Approving-timesheets) | 403, arama özeti |
| Homebase, ADP, Gusto | Yöneticinin kendi kaydı hakkında kaynak **bulamadım** | Yok |

**Okuma:** yönetici engeli için kaynak güçlü. "Üst yetkili serbest" için kaynak zayıf; bunu piyasaya dayandırmıyorum.

**Sahip varsayılanı, soru değil, bildirim:** bağlı sahip kendi kaydına karar verebilir. Gerekçe piyasa değil, **ikinci kişinin olmaması**: 4/4 işletme tek sahipli; düz yasak sahibin işaretli kaydını çözülemez bırakır (§4.6'nın lafzı korunur, işlevi bozulur). Damga, tek sahipte kendi kendini etiketlemedir: bağı kuran da etiketlenen de aynı kişi. Delil gerekçesi (§4.3) bu yüzden zayıftır; damga yalnız iki sahipli işletmede öteki sahibe bir şey söyler. **Bu muafiyet, ADR 0004 Y-C'nin kararını daraltır:** Rules metni T148-0'a kadar *"nobody"* diyordu, `open-questions.md:51` istisnasız yazar. Soru olarak sorulmaz, ama bildirilirken bu daralma açıkça söylenir.

## 3. Seçenekler

İlk soru *aynı kişiyi nasıl biliriz*. (`admin_users.employee_id` eledim: sıcak tablonun sütun düzeyi yetkilerini genişletir, bir yöneticiye tek satır bağlar. "Engel yok, yalnız damga" eledim: asıl tehdidi kapatmaz ve yine bağa muhtaçtır.)

| | **A′** `employees.admin_user_id` | **B** `employees.admin_identity_id` | **C** e-posta eşitliği | **Erteleme** (yalnız T148-0) |
|---|---|---|---|---|
| Kapatır | Bağlı yöneticinin üç yolu | Aynı + işletmeler arası tanıma (gereksiz: kayıt ve karar hep aynı tenant, ADR 0027 K-1a) | Aynı adreslileri | Hiçbirini; yalnız yanlış vaadi |
| Açık | Eşlenmemiş hesap, hesap paylaşımı, ikinci satır | Aynı + `identity_id` NULL satırlar | Adres boş ya da farklıysa açık. Aynı adres yalnız API (`internal/domain/tenant/staffapi.go:439`) ve çalışan davet e-postasında reddedilir (`internal/invite/email.go:447`); yönetici daveti (`internal/adminauth/admininvite.go:273`) ve panel eklemesi bakmaz. Canlıdaki 0, yönetici olmamasının sonucu | Hepsi, ilk yönetici gelince |
| §4.5 metni | Değişmez | **Değişir**: kullanıcı onayı + ADR 0031 | Değişmez | Değişmez |
| Veri | Sütun + bileşik FK; geçmiş satıra dokunmaz | Kapsamsız tabloya FK; kişi anahtarı çalışan tablosuna iner | Yok | Yok |
| Yanlış sonuç | Yanlış bağ (§8) | Yanlış bağ | Ortak posta kutusu | Yok |
| Maliyet | Orta | Yüksek | Düşük, ama zayıf kanıt | Çok düşük |

**Öneri A′.** C kapı olamaz: adres boş, farklı ya da ortak olabilir. Adres önerisi ise terfide değerlidir: kadrodaki Mia aynı adresle yönetici yapılabilir, ve o zaman Managers satırı sahibe bağ önerebilir. Karar yine sahibindir.

## 4. Önerilen karar

**Bağ.** `employees.admin_user_id uuid NULL`, bileşik FK `(admin_user_id, tenant_id) → admin_users (id, tenant_id)`, `ON DELETE RESTRICT`. Çapraz tenant bağı FK reddeder (ölçüldü). **Bağı iki ifade yazar:** sahibin bağ ifadesi (`a.mayInvite` kapısı, `internal/handler/admininvites.go:168`) ve eklemede taşıma (`CreateEmployee`, `db/queries/employees.sql:930`; §6). Ekleme her yöneticiye açıktır, ama taşıma yeni bağ yaratmaz, sahibin kurduğunu kopyalar. Tarama testi tam bu iki ifadeyi pinler. Bağ kurmak daraltır, K-26 zinciriyle askıda da geçer; çözmek durur (`internal/handler/adminmembers.go:31-36`). Definer ve sütun listesi önermiyorum: Tehdit 1'e karşı kazanç yok, §4.5'e yeni istisna olur.

**Onay ekranı.** İki tarafın adı ve adresi, ve *"bu bağ doğruysa bu kişi geçmişte N kez kendi kaydına karar vermiş"*. İki aktif satır aynı yöneticiye bağlanınca uyarı.

| Karar veren | Bağlı, kendi kaydı | Sonuç |
|---|---|---|
| Yönetici | İşaretli kayıt, saat talebi | **Red** (onay ve ret). Kayıt kuyrukta kalır; sahip ya da başka yönetici karar verir |
| Yönetici | Elle giriş | **`flag` yazılır** (soru 2), kuyruğa düşer |
| Sahip | Üç yol | **İzin**, `self_decided: true` audit `detail`'inde |
| Herhangi biri | Başkasının kaydı | Bugünkü gibi |
| Herhangi biri | **Bağsız hesap** | Bugünkü gibi; kalan riskin kendisi |

**Guardrail.** `sys:no-self-review` `sys:` sınıfında kalır (emsal `sys:policy-edit-owner-only`, ADR 0004:160-164), sayı 11 kalır. Yalnız karar yollarında koşar; `Match` bağlı yöneticiyi ve rolü okur, rol yoksa reddeder. `Context.SubjectEmployeeID` yeniden adlandırılır. Elle giriş guardrail değildir: alan kuralı + tetikleyici.

**Rules ekranı dürüstlüğü.** Kapatma düğmesi yok, ama bağsız hesapta guardrail ateşlemez ve bağ sahibin elindedir; `Make owner` da aynı sonucu verir (`dashboard.go:281`). Ekran *"bağlı hesap"* diye konuşur, *"nobody"* demez.

**Tetikleyiciler.** `tappa_check_transaction_review` ve `tappa_check_punch_claim_decision` değişir; `transactions`'a `BEFORE INSERT … WHEN (NEW.channel = 'manual')` eklenir: bağlı yönetici `ok` yazamaz, tap yolu bu tetikleyiciyi çalıştırmaz. `SECURITY INVOKER` açıkça yazılır (ölçüldü: `prosecdef=f`, `tappa_app` `role` okuyor). **TOCTOU:** READ COMMITTED altında rol ve bağ tek ifadede, çalışan satırına `FOR SHARE OF e`. Rol değişimiyle yarış açıkça kabul edilir: iki eylem de sahibin.

**Bağ izi.** İki tetikleyici eski ve yeni değeri `audit_log`'a yazar, `actor_id` NULL (ölçüldü: sütun nullable, `tappa_app` sütun düzeyi yazar, silemez): `AFTER UPDATE OF admin_user_id … WHEN (OLD.admin_user_id IS DISTINCT FROM NEW.admin_user_id)` ve `AFTER INSERT … WHEN (NEW.admin_user_id IS NOT NULL)`. Tek bir `INSERT OR UPDATE` olamaz: PostgreSQL `WHEN`'de `OLD`'u reddeder (ölçüldü, 17). Aktörlü satırı uygulama ayrıca yazar.

**Ret izi.** Kararın içinde reddedip audit yazmak çalışmaz: hata işlemi geri alır (`internal/db/tenant.go:74-76`; `RecordTx` aynı işlemde, `internal/domain/review/review.go:168-170`, `manual.go:281-283`). Geri almadan **sonra** kendi işleminde `audit.Record` (`internal/audit/audit.go:83`; emsal `internal/handler/employeeemail.go:43`). İki ret yolu aynı sonuca iner: alan kapısı ve tetikleyici. `detail`: `employee_id`, yön, `occurred_at`, yol. Not metni yok.

**İlk sürümde rapor etiketi yok (eski T148-5 çıktı).** Yalnız audit `detail`. "Kendi kaydı" rapor satırı ve CSV çıkarıldı: tek sahipte kendi kendini etiketler, yanlış bağda geçmişe haksız suçlama yazar, ve CSV'nin `entered_by` duvarıyla çelişir (`internal/handler/reportscsv.go:32-44`).

**Bekleyen kayıt görünür.** Kuyrukta kendi kaydında düğme yerine *"başka bir yetkili karar verecek"*. Panel ana sayfasına *"N kaydı siz karar vermelisiniz"* bandı öneriyorum. Bekleyen saat CSV'de *Hours awaiting a decision* satırındadır (`reportscsv.go:796`). Geç kalan elle düzeltmenin sınırı 90 gündür (`manual.go:218`). Maaş etkisi hukuki görüş değildir.

## 5. Güvence: neye karşı, neye karşı değil

ADR 0036'nın biçimiyle; "yetki değişmez" bir artı değildir.

| | **Tehdit 1:** `tappa_app` olarak keyfî SQL yürüten süreç | **Tehdit 2:** binary hatası (kapı unutulur) |
|---|---|---|
| Bağ | `UPDATE employees SET admin_user_id = NULL` yapabilir (tablo düzeyi `00003:105`) | İki yazan sqlc ifadesi tarama testiyle pinli (§4) |
| Rol | `UPDATE admin_users SET role='owner'` yapabilir (`00017:380`, ölçüldü) | Değişmez |
| Kimlik | `reviewer_id`, `entered_by` FK'siz, keyfî id yazar (ölçüldü) | Oturumdan gelir |
| Tetikleyici | **Engel değil** | **Engel:** doğru id ile unutulmuş kapıyı reddeder |
| İz | Bağ değişimi `audit_log`'da kalır, silemez. Rol değişimi izsiz | Tam |

Sonuç: bu tasarım iş kuralıdır ve hataya karşı kemerdir; ele geçirilmiş bir uygulama kimliğine karşı güvence değildir. Public metinde güvence diye yazılmaz.

## 6. Atlatma ve çaresi

**Üç POST (incelemede, kod okundu, ölçülmedi):** bağlı yönetici kendi satırını deaktive eder (deaktivasyon her yöneticiye açık: `employeeemail.go:25`, `dashboard.go:193`), aynı ad ve adresle yeni satır açar (`db/migrations/00037_add_external_refs_and_the_invite_queue.sql:103-105`, `IsAdministratorAddress` yalnız API), elle saat yazar (`transactions.sql:473-489`). Bağ eski satırda kalır.

**Y-D altında kabul edilmiş sayılamaz:** Y-D'nin M6-11 sinyalleri 2026-08-12'de düştü (`docs/plan/m6-dashboard.md:6604-6608`, `:6704`); `docs/plan/open-questions.md:52` bayat.

**Çare:**
- Yönetici kendi bağlı satırını deaktive edemez; sahip edebilir.
- Yönetici panelde bir yetkilinin adresiyle satır ekleyemez; farklı ya da boş adresle ekleyebilir.
- Sahibin kurduğu bağ, aynı adresle panelden açılan yeni satıra taşınır. Bu otomatik bağ değildir: yalnız kısıtlar. Aynı adreste birden çok pasif bağlı satır varsa en yeni `deactivated_at`'lı satırın bağı alınır, eşitlikte en büyük `id`.
- API taşımaz (`CreateEmployeeFromAPI`, `employees.sql:1153`): anahtarları yalnız sahip verir, yetkili adresi orada 422'dir. API'den yeniden açılan satır bağsız başlar; sahip bağlar.

Bağ yeniden işe alımı kendiliğinden kapsamaz: yeni satır (ADR 0010:191) farklı ya da boş adresle bağsız başlar. **Yönetici farklı ya da boş adresle yeni satır ekleyebilir; bu kalan risktir.** Hareket izli olur, kapanmaz.

## 7. Kırmızı çizgiler ve kişisel veri

- **§4.1, §4.2:** biyometri ve GPS yok.
- **§4.3:** `transactions`, `transaction_reviews`, `punch_claim_decisions` satırları ve sütunları değişmez.
- **§4.5:** metin değişmez; tenant içi, definer yok.
- **§4.6:** tap kaydı kuyrukta kalır; elle beyan kaybolmaz (soru 2).
- **§5:** tablo ve `tap.Decide` değişmez. "Manuel kayıt" maddesine bir cümle: kullanıcı onayı gerekir.
- **GDPR (hukuki görüş değil):** yeni veri kategorisi yok, kullanıcıdan alan toplanmaz. Yeni olan tek şey iki kaydın aynı kişi olduğu olgusu. Yalnız o işletmenin panelinde; operatör, holding, akış rolleri sütunu okuyamaz (ölçüldü). Q13 anonimleştirmesinde bağ NULL'lanır (`open-questions.md:92`'ye not). Gizlilik metni kontrol edilmedi.

## 8. Kalan risk

- **Eşlenmemiş hesap, en büyüğü.** Sahip bağ kurmazsa koruma o hesapta yok.
- **Yanlış bağ.** Gerçek satır korumasız kalır, yanlış kişi engellenir. Onay ekranı azaltır, kapatmaz.
- **İki yöneticinin birbirini onaylaması.** Kapatılmaz; audit görür.
- **Farklı ya da boş adresle ikinci satır** (§6). **Hesap paylaşımı:** hiçbir seçenek kapatmaz.
- **Mekân ağı (incelemede, kod okundu, ölçülmedi):** `saveVenue`'da rol kapısı yok (`internal/handler/locationactions.go:109`). Yönetici kendi IP'sini ekler, fotoğraflanmış QR'la evden `ok` alır. Ayrı karar; T148'i bloke etmez.
- **Yokluktaki sahip.** Başka yetkili yoksa yöneticinin işaretli saati sahip bakana dek saate girmez.
- **`reviewer_id` ve `entered_by` FK'siz** (`00006_create_admin_users.sql:217-223`, ölçüldü): ayrı backlog adayı.

## 9. Kullanıcı kararları (2026-11-07)

Kararlar: **1 (a), 2 (a), 3 (a).** Sahip muafiyeti (§2) bildirildi, itiraz yok. Aşağıda sorular, sorulduğu haliyle durur.

**1. Koruma ne zaman çıksın?** Yanına: yakında saat basan bir vardiya müdürü atanacak mı? Takip davet formundaki uyarıya ve bu cevaba dayanır: "ilk saat basan yöneticiden önce" tarihini tutan mekanik bir kapı yok; bu (a) için de geçerli.
- **(a) Seçildi.** Metin düzeltmesi hemen; bağ ve engel ilk saat basan yöneticiden önce.
- (b) Hepsi hemen. Canlıda korunacak kimse yokken iş yapılır.
- (c) Talep gelene dek yalnız metin. En ucuz; yönetici tek basışla doğar.

**2. Yönetici kendi saatini elle girince ne olsun?**
- **(a) Seçildi.** İşaretli yazılsın, başka yetkiliye düşsün. Beyan kaybolmaz, §4.6 ile aynı desen. Ölçüldü, **yalnız okuma tarafı ve tek şekil** (`ok` giriş + elle konmuş `flag` `manual` çıkış): kuyrukta görünür; beklerken 8 sa *awaiting*, onayda 8 sa *worked*, retle 8 sa *refused*. Yazma yolu yok: bugün `flag` yazan ifade yok (`internal/domain/manual/manual.go:45-48`); T148-3'ün işi. Bu seçeneği üstün kılan hiç dokunulmamış vardiya **ölçülmedi** (§12). Akışa düşmez (`TestPunches_NoManualNoRejected`). `db/queries/transactions.sql:399-408`'in iki gerekçesi var: ilki (*onaylayacak kişi yazanın kendisi*) burada geçersiz, çünkü tetikleyici yazanı reddeder ve son sahip düşürülemez (`internal/adminauth/membership.go:249-251`). İkincisi (Q18: toplam onaya kadar eksik kalır) **durur**: bağlı yönetici için bilerek kabul edilen bedel.
- (b) Reddedilsin. Dürüst cümleyle: saat talebi yalnız dokunuşun eksik yarısı içindir (`internal/domain/claim/claim.go:1-4`), kart sonuç ekranında çıkar, iki gösterim (`internal/domain/claim/offer.go:26-32`), kayıt başına bir hak, ret kesin (`00068:114-115`); hiç dokunulmamış vardiya, yanlış saat, reddedilmiş talep kapsam dışı; canlıda hiç karar üretmedi.
- (c) Serbest, yalnız görünür. Diğer iki kapının anlamı kalmaz.

**3. Bağ nasıl kurulsun?**
- **(a) Çalışan satırında "Invite as manager" davet formunu önceden doldurur; kabulden sonra satır bağlanana kadar Managers'ta "bağlı değil" görünür; kapatma cevabı yok; otomatik bağ yok. Seçildi.** Bağı sahip kabulden sonra ayrıca kurar. Kadroda olmayan yönetici hep "bağlı değil" görünür: NULL bağ "cevap yok" ile "kadroda değil"i ayıramaz, bu bedel kabul edilir.
- (b) Yalnız Managers sayfası. En sade, en çok unutulan.
- (c) Bağ davete taşınır, davet formunda zorunlu cevap. En güçlüsü; ama `admin_invites`'ta çalışan sütunu yok (`db/migrations/00077_invite_administrators.sql:139-155`), sütun ve kabul fonksiyonu M14 matrisine dokunur ve §7'deki "definer yok" düşer.

(a)'da davet çalışan id'si **taşımaz**. Ad çakışmaz: "Make manager" bugün sahibi yöneticiye indiren düğmedir (`internal/handler/dashboard.go:422`, `web/templates/pages/admininvites.templ:149`).

## 10. Plan

Yalnız genişletme. Migration Job rollout'tan önce koşar (ADR 0031 §2); bütün bağlar NULL iken davranış bugünküyle aynıdır. **İki deploy yeter:** T148-1, sonra T148-2/3/4. Geri dönüş: bağlar varken T148-3 öncesine dönülürse elle giriş 500 verir (`manual.go:831-838`, site kolu `:888-891`).

| Kart | İş | Test |
|---|---|---|
| **T148-0** | Karardan bağımsız, ayrı dalda, migration yok. Rules metni + davet formu cümlesi (§11) | Çizim testi: motor cümlesi ve iki etiket bu guardrail'de yok |
| **T148-1** | Sıcak tablo başına ayrı dosya, ifade başına `lock_timeout`, FK `NOT VALID` ve `VALIDATE` ayrı işlemlerde, `CONCURRENTLY` indeks (`00070` deseni), bağ izinin iki tetikleyicisi, üç karar tetikleyicisi, `EXECUTE` yalnız sahipte (`00061`), Down dolu | Gerçek Postgres, `tappa_app`: üç yol × iki rol × bağlı/bağsız, onay ve ret ayrı; RLS (filtresiz); çapraz FK; tap kanalı tetiklemez; bağ izi; `internal/db/triggerexecute_test.go:142-145` güncellenir |
| **T148-2** | Guardrail: alan adı, rol, fail-closed | Tablo: yönetici/kendi, sahip/kendi, rol yok, farklı kişi, bağ yok; `TestGuardrails_SelfReviewOnlyWhenSamePerson`, `TestGuardrailGuards_ReCheckedWhenTheShippedSetChanges` güncellenir |
| **T148-3** | `review`, `claim`, `manual`: bağı oku, karar; ret izi; `self_decided`; **`flag` yazıcıları** (soru 2 (a) ise): `InsertManualTransaction`, `InsertManualTransactionAtAnchor`, `InsertManualSiteTransaction`; `manual.go:45-48` yorumu güncellenir; **hata eşleme** (claim tetikleyicisinin yeni mesajı `ErrRefused`'a düşer: `decide.go:393-396`; manuelde eşleme yok, 500: `manual.go:831-838`, `:888-891`; ayrı cümleler `internal/handler/review.go:475-482`, `claimqueue.go:479-481`) | Ret izi **iki yolda**: alan kapısı ve tetikleyici; kayıt yazılmaz, tek audit satırı; §12'deki `flag` şekilleri; `TestDecideDB_TheSelfDecisionCheckFiresOnlyOnAForgedIdentity` gerçek bağa çevrilir |
| **T148-4** | Bağ ekranı (soru 3), onay ekranı, §6 çareleri, band. Taşımanın `deactivated_at` sıralaması `NULLS LAST`. "Bağlı değil" **aktif** satır üzerinden hesaplanır: yalnız pasif satıra bağlı yönetici bağlı görünmez. "Invite as manager" yalnız `mayInvite`'e çizilir; URL adres taşımaz: çalışan id'si gider, ad ve adresi sunucu okur. skill `tappa-brand` | Gerçek HTTP: sahip kapısı, 403 + iz, üç POST'un bağlı yöneticide kapanması, askıda bağ kurma; taşıma, seçim kuralı, API'nin taşımaması; iki yazıcı tarama testi |
| **T148-6** | CLAUDE.md §5 (onaylı metin), ADR 0004 ve 0032 notları, ADR 0031 `:124` notu, `docs/plan/m3-policy-motoru.md:331`, `:355`, `open-questions.md:51`, `:52`, `:92`, kod yorumları, T148 kapanışı. *"Nobody can decide"* yasağı kalır. Koruma çıkınca doğrulanır: `web/templates/pages/securityview.go:657` *"of the eleven guardrails"* ve `web/templates/pages/productview.go:370` *"cannot be switched off"*; bugün self-review vaadi vermiyorlar, ama *eleven* planlı basamağı sayıyor | `TestEveryNamedTestExists`, `TestSecurityPage_SaysNoneOfTheRefusedPhrasings` |

**Kabul kriterleri**
1. Bağlı yönetici iki karar yolunda reddedilir, elle girişi `flag` olur: alan katmanı ve o atlanınca tetikleyici.
2. Bağlı sahip üç yolda geçer; audit `detail`'inde `self_decided: true`.
3. Bağsız hesabın davranışı bugünküyle aynıdır.
4. Bağı yalnız iki pinli ifade yazar (sahibin ifadesi, eklemede taşıma); her değişim veritabanı izi bırakır.
5. Reddedilen deneme, kayıt yazılmadan tek audit satırı bırakır.
6. Rules cümlesi davranışla aynıdır: `Match`'i bağlı yönetici, bağlı sahip ve bağsız hesapla koşan pin testine bağlanır.
7. `tap.Decide` testleri (`TestDecide_DirectionNilForNonRecordVerdicts` ve ailesi), `TestEndpointState_NeverPaysAVerdictItDoesNotKNOW`, `TestPanel_CallsThePolicyEngineNowhere`, tap ve sonuç ekranları değişmez.

## 11. T148-0 (uygulandı, 2026-11-07)

Migration yok. Tap, sonuç ve aktivasyon ekranına dokunulmadı. Satırlar uygulanmış hale göredir.

- **Planlı basamak:** `internal/domain/tenant/rulebook.go` `guardrailRules` ve `guardrailWhen` metinleri, `guardrailStage` (`tenant.Guardrail.Stage`). T148-2 ve T148-3 guardrail'i koşturunca girdi ve metinler kalkar.
- **When:** *Planned: a manager decides their own record*
- **Rule:** *Not enforced yet. Taptime does not yet know which manager login belongs to which employee, so someone who is both can decide their own flagged record or type their own hours. Each decision is recorded with who made it.*
- **Çizilmez** (`web/templates/pages/policies.templ`, `g.Stage == ""` kapıları): bu basamakta sonuç etiketi, *Cannot be switched off* ve motor cümlesi. `internal/policy/guardrails.go`'daki Reason değişmedi: yalnız motor cümlesinde çizilirdi.
- **Sayfa girişi**, *"each of them is a promise"*den sonra tek cümle: *The last one below is still planned: it is not enforced yet, so it is not yet one of these promises.*
- **Panel özeti** (`internal/handler/policies.go`): yalnız `Stage == ""` olanları sayar: *10 guarantees · 1 planned · …*. İlk sürüm 11 diyordu ve giriş cümlesini yalanlıyordu. `tenant.Guardrail` sayısı 11 kalır.
- **Managers davet formu** (`web/templates/pages/admininvites.templ`, `managersInviteForm`), `md:basis-full` ile tek cümle: *A manager can currently decide their own flagged record and type their own hours.*
- **Pinler:** `TestGuardrailPlanned_TheSelfReviewPlateSaysItIsNotEnforced`, `TestRulesScreen_ThePlannedRungDrawsNoPromiseAndTheOthersKeepTheirs`, `TestRulesScreen_TheSummaryDoesNotCountAPlannedGuaranteeAsAGuarantee`, `TestManagersForm_TheWarningIsInTheFormAndNowhereElse`.

## 12. Ölçülmedi

Atlatma zinciri ve mekân ağı (kod okundu) · `punch_claim_decisions` tetikleyicisinin yeni hali · **`flag` + `manual`:** giriş satırı, hiç dokunulmamış vardiya (giriş + çıkış manuel; soru 2 (a)'yı üstün kılan durum), `CorrectionsOnlyShorten` düzeltme şekli (`manual.go:89-112`), yazma yolu, peer mekânı kolu · `ALTER`'ların canlı kilit süresi · guardrail değişikliği ve ekranlar · When I Work üst rolleri, Homebase, ADP, Gusto.

Prototip (geçici DB, depoya girmedi): bağlı yönetici onay, ret ve kendi manueli reddedildi; sahip üçünde geçti; bağsız hesap geçti; yöneticinin başkasına kararı geçti. Tetikleyicide `flag`'e izin veren kol (soru 2'nin önerisi) denenmedi: prototip o manueli düz reddediyordu. Tek şekilli `flag` + `manual` çıkışı gerçek `ledger.Reader` ile okundu (satır doğrudan yazıldı); deneme testi silindi.
