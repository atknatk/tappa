# ADR 0037 — Yönetici kendi kaydına karar veremez: panel hesabını çalışan satırına bağlamak

- **Durum:** **Önerilen.** Karar kullanıcının; kod ve migration yazılmadı. İki bağımsız inceleyiciden
  sonra §9'daki üç soruyla kullanıcıya gider.
- **Tarih:** 2026-11-06
- **Backlog:** T148
- **İlgili:** ADR 0004 (Y-C, `sys:no-self-review`) · ADR 0005 (Y-D) · ADR 0009 · ADR 0010 · ADR 0027 (K-1a) ·
  ADR 0031 · ADR 0032 §C.4 (G-O1) · CLAUDE.md §4.3, §4.5, §4.6, §5, §6
- **Atıflar** `origin/m14-identity` `ffbf1808`'e göredir.

## Özet

Bugün bir yönetici kendi işaretli kaydını onaylayabilir, kendi saat talebini onaylayabilir ve kendi saatini
onaysız elle girebilir. Koruma üç yerde yazılı ama hiçbiri çalışmıyor: farklı tablolardan gelen iki id'yi
karşılaştırıyorlar.

**Öneri:** `employees.admin_user_id` ile açık bir bağ kurulur, bağı yalnız sahip yazar. **Yönetici** kendi
kaydına karar veremez ve kendi saatini elle giremez. **Sahip** verebilir, ama kararı "kendi kaydı" diye
görünür. Eski kayıtlara dokunulmaz; bağ kurulunca geçmişteki öz-kararlar da raporda okunur hale gelir.

## 1. Sorun

### 1.1 Koruma neden ölü

Kaydın sahibi `employees.id`, karar veren `admin_users.id`. İkisi rastgele üretilen ayrı id'ler; aynı kişide
bile eşit değiller. Aralarında sütun, FK ya da sorgu yok.

| Yer | Ne karşılaştırıyor | Bugünkü sonuç |
|---|---|---|
| `db/migrations/00005_create_transactions_audit_reviews.sql:354` | `NEW.reviewer_id = v_employee_id` | Hiç tutmaz. `reviewer_id` FK'siz kaldı (`00005:264-267`, `00006_create_admin_users.sql:217-223`) |
| `db/migrations/00068_create_punch_claims_and_their_decisions.sql:313` | `NEW.decided_by = v_employee` | Hiç tutmaz; `decided_by` `admin_users`'a FK'li (`:251`). Dosya bunu kendi söyler (`:268-271`) |
| `internal/domain/claim/decide.go:292` | `d.DecidedBy == c.EmployeeID` | Hiç tutmaz; yalnız sahte satırla tetiklenir (`TestDecideDB_TheSelfDecisionCheckFiresOnlyOnAForgedIdentity`) |
| `internal/policy/guardrails.go:610-621` `sys:no-self-review` | `ReviewerID == SubjectEmployeeID` | Hiç koşmaz. Üretimde bu alanları dolduran çağrı yok; tek kullanım `internal/domain/tenant/rulebook.go:933`'teki sentetik yoklama. Panel motoru çağırmaz (`TestPanel_CallsThePolicyEngineNowhere`) |

Kod ve dokümanların bir kısmı boşluğu kabul eder (`internal/domain/review/review.go:88-92`,
`internal/handler/review.go:475-478`, `internal/domain/claim/claim.go:27-32`, ADR 0032 G-O1). İki yer ise hâlâ
çalışıyormuş gibi söyler:

- **Müşteriye görünen Rules ekranı:** *"nobody approves their own record"* (`internal/domain/tenant/rulebook.go:863-864`,
  `:887`; `web/templates/pages/policies.templ:418`). LP-07 bu iddiayı public sayfalardan kaldırdı
  (`web/templates/pages/securityview.go:213-215`), panel metni kaldı. **Bugün yanlış bir vaat.**
- `docs/plan/open-questions.md:51` (Y-C ✅) ve `db/queries/reviews.sql:178-183` (*"what make it a guarantee"*).

**M14 bunu çözmedi.** T148'in eki ve `claim.go` tek kimliğin boşluğu kapatacağını varsayıyor. ADR 0031 yalnız
yönetici satırlarını işletmeler arası bağlar; *"Çalışan kimliği … etkilenmez"* (ADR 0031 §3, `:124`). `employees`'te
kimlik sütunu yok.

### 1.2 Kim neyi yapabiliyor

Ölçüm: geçici Postgres, goose 87, `tappa_app`, tenant bağlamı, 2026-11-06. Aynı kişi: Mia hem yönetici hem çalışan,
aynı adres.

| Yol | Rol kapısı | Mia kendi kaydına | Saate etkisi |
|---|---|---|---|
| İşaretli kayıt: `POST` review (`internal/handler/dashboard.go:186`, `internal/handler/review.go:422-428`) | Yok, her yönetici | **Ölçüldü: onay yazıldı** | `flag` + `approved` sayılır (`internal/domain/ledger/report.go:819-832`) |
| Saat talebi: `claimDecision` (`internal/handler/claimqueue.go:248`, rol notu `:56`) | Yok, her yönetici | Kod okundu, aynı boşluk (`internal/domain/claim/decide.go:292`) | Onay `manual` satırı yazar (`internal/domain/claim/decide.go:333-341`) |
| **Manuel kayıt** (`internal/handler/dashboard.go:409`, `internal/handler/manualentry.go:352-358`) | Yok, her yönetici | **Ölçüldü: `verdict='ok'` yazıldı, onay adımı yok** | Hemen sayılır (`db/queries/transactions.sql:473-479`) |
| Rapor düzeltmesi | Ayrı yol yok | Düzeltme, yeni manuel satırdır (`internal/domain/manual/manual.go:89-112`) | Manuelle aynı |
| Dış API (`internal/handler/api/surface.go:192-197`) | Sahibin anahtarı | Saat yazan veya karar veren uç yok | Yok |
| Sahte çalışan açmak (`employeeAdd`, `internal/handler/employeeactions.go:207`) | Her yönetici | Kabul edilmiş risk Y-D (`docs/plan/open-questions.md:52`) | **T148 kapatmaz** |

🔴 **En önemli sonuç:** yalnız onay yollarını kapatmak işe yaramaz. Engellenen yönetici aynı saati manuel
olarak, onaysız yazar. Manuel yol kapsam dışı kalırsa diğer ikisi vitrin olur.

**Roller:** `owner` ve `manager` için aynı (kod iki rolü ayırmıyor). **Küçük işletme:** kayıt sihirbazı sahibi
çalışan olarak açmaz (`internal/domain/signup`'ta `employees` yazımı yok). Sahip ancak kendini çalışan olarak
eklediyse kendi kaydı olur; o zaman kararı verecek ikinci kişi olmayabilir.

### 1.3 Tehdit

Asıl tehdit, **ücreti ödeyen sahibe karşı kendi saatini şişiren yönetici**dir. Sahibin kendi kaydı başka bir
sorundur: burada aldatılan yok, risk kaydın **delil değeridir** (§4.3). Çözümü bu ayrım belirler.

## 2. Kapsam sorusu: tek yetkili kendi kaydına kilitlenirse

Düz yasak, Rusty Bar gibi tek sahipli bir işletmede sahibin işaretli kaydını sonsuza dek kuyrukta bırakır.

- **§4.6 (kayıt kaybolmaz):** kayıt kaybolmaz ama çözülemez olur. Saate girmez (`HoursAwaiting`), rapor hep
  *awaiting* der, kuyruk hiç boşalmaz. Lafzı korunur, işlevi bozulur.
- **§5 (guardrail kapatılamaz):** müşteri bir guardrail'i kapatamaz. Kodda sabit, rol koşullu bir `Match`
  kapatma değildir; hiçbir tenant ayarı yok. Ama Y-C'nin *"nobody"* sözü daralır, bu yüzden **kullanıcı kararıdır**.
- **§5 (sessiz onay yok):** sahibin öz-kararı damgalı ve audit'li olduğu için sessiz değildir.

### Piyasa deseni (araştırma 2026-11-06)

| Ürün | Bulunan | Güven |
|---|---|---|
| [Deputy](https://help.deputy.com/hc/en-au/articles/13841429485967-Approve-Timesheets-and-Export-Timesheets-Permissions) | Rol izni. System Administrator herkesi onaylar, Supervisor yalnız Employee düzeyini; özel rolde "kendileri dahil" izni var; izni yoksa [Approve yerine Update görür](https://help.deputy.com/hc/en-au/articles/4689553875471-Approving-timesheets) | Orta: sayfalar 403, arama özetinden okundu |
| [When I Work](https://help.wheniwork.com/articles/role-permissions-reference/) | Supervisor için kutucuk: *"Approve button for their own timesheet"*. Manager/Admin için söz yok, varsayılan yazmıyor | Yüksek (Supervisor), bilinmiyor (üst roller) |
| Homebase ([onay](https://joinhomebase.com/blog/new-homebase-timesheet-approvals/), [izinler](https://support.joinhomebase.com/hc/en-us/articles/115000198372-Edit-an-employee-s-information)) | Onay kilitler; sahip her şeye erişir. Kendi kartı hakkında kaynak **bulamadım** | Düşük: arama özeti |
| [ADP](https://otcdc1.adp.com/ezLMHelp/V18_30/Standalone/UKFeatureSet/StandardUI/en-GB/ss_help/ss/timecard_approvals-intro.htm) | Onay kuralları şirket politikasına bağlı. Yöneticinin kendi kartı hakkında kaynak **bulamadım**, SSS sayfası açılmadı | Düşük |
| [Gusto](https://support.gusto.com/article/116295878100000/Review-edit-and-approve-Gusto-Time-Tracking-hours) | Yönetici doğrudan bağlılarını onaylar. Yöneticinin kendi saati hakkında kaynak **bulamadım**; "admin onaylar" yalnız çıkarım | Düşük: 403, arama özeti |
| [Roubler](https://support.roubler.com/hc/en-us/articles/31309756452249-Prevention-of-Managers-Approving-own-Timesheets) (ek) | **Varsayılan: yönetici kendi kaydını onaylayamaz**, istisna izinle | Orta: 403, arama özeti |
| [Hive](https://hiveteams.canny.io/changelog/ability-for-admins-to-approve-their-own-timesheets) (ek) | Admin öz-onayı sonradan eklendi; gerekçe: tek çare bir ekip arkadaşına onaylatmaktı | Yüksek, ama saat takip ürünü değil |

**Okuma:** düz yasak ana akım görünmüyor. Desen **role bağlı**: alt yönetici engelli ya da izne bağlı, en üst yetkili
serbest. İki sınır: beş ürünün üçünde doğrudan kanıt yok; **"kendi onayı" damgası gören ürün bulamadım**, o bizim
eklememiz (§4.3 delil gerekçesiyle). Bu ürünlerde yönetici ile çalışan tek hesaptır, eşleme ekranı yoktur; bizde iki
kimlik olduğu için bağ gerekir ve sürtünmesi düşük tutulmalıdır.

## 3. Seçenekler

İki ayrı soru var: **aynı kişiyi nasıl biliriz** (A, A′, B, C) ve **bilince ne olur** (engel ya da D). D tek başına
çözüm değildir: kimi damgalayacağını bilmek için önceki dördünden birine muhtaçtır.

A′ brief'teki A'nın aynı bağı ters taraftan kuran biçimidir; **öneri odur**. Prototip yalnız A′ için ölçüldü.

| | **A** `admin_users.employee_id` | **A′** `employees.admin_user_id` | **B** `employees.admin_identity_id` | **C** e-posta eşitliği | **D** engel yok, damga |
|---|---|---|---|---|---|
| Kapatır | Bağlı yöneticinin 3 yolu | Aynı + yeniden işe alınan kişinin eski ve yeni satırı | Aynı + kişi işletmeler arası tanınır | Aynı adreslileri, bağ işi olmadan | Hiçbirini; yalnız görünür yapar |
| Açık: eşlenmemiş hesap | Açık | Açık (uyarı §4) | Açık; ayrıca `identity_id` NULL satırlar | Adres boş ya da farklıysa açık (`employees.email` NULL olabilir: `00003:53`) | Açık |
| Açık: hesap paylaşımı | Açık | Açık | Açık | Açık | Açık |
| Açık: ikinci panel hesabı | Açık, ama yönetici davet edemez (`internal/handler/adminmembers.go:24-29`) | Aynı | Aynı adresle kapanır, başka adresle açık | Açık | Açık |
| Açık: ikinci çalışan satırı (Y-D) | Açık | Açık | Açık | Farklı adresle açık | Açık |
| Migration, RLS | Sütun + bileşik FK, sıcak tablo; tek değer tutar, yeniden işe alımda eski satır düşer | Sütun + bileşik FK (hedef `admin_users_id_tenant_key` hazır); `employees` RLS'i yeterli | Tenant tablosundan kapsamsız tabloya FK | Yok | Karar tablolarına sütun ya da yok |
| `tappa_app` | Sütun düzeyi `SELECT`/`UPDATE (employee_id)` genişler (`00017:379-381`, `00069:248-251`) | **Yetki değişmez**: `employees` tablo düzeyi (`00003:105`) (ölçüldü) | İki sorun: tablo düzeyi yazma ile tenant başkasının kimliğini yazabilir (`00069:243-245`'in anlattığı saldırının ikizi); karşılaştırma için `identity_id` gerekir, onu yalnız `TouchAdminSession` seçebilir (`db/queries/admins.sql:134-136`) | Yok | Yok |
| Kapalı `identity_*` listesi, §4.5 metni | Değişmez | Değişmez | **Değişir**: kullanıcı onayı + ADR 0031 değişikliği; kişi korelasyon anahtarı çalışan tablosuna iner | Değişmez | Değişmez |
| Geçmiş satırlar | Join ile okunur | Join ile okunur (ölçüldü) | Join ile okunur | Bugünkü adresle okunur, adres değişirse kayar | Damga eskiye işlemez |
| §4.3 değişmezlik | Dokunmaz | Dokunmaz | Dokunmaz | Dokunmaz | Sütun gerekiyorsa ADD COLUMN (yeni satırlar) |
| Operatör görünümü | Değişmez | Değişmez: `tappa_opdefiner` sütunu okuyamaz (ölçüldü) | Değişmez | Değişmez | Değişmez |
| `claim.Decide` | Bağı okur | Bağı okur | Kimliği okur; bugün yasak okuma | Adresleri karşılaştırır | Yasağı kaldırır |
| `sys:no-self-review` | Guardrail kalır, koşar | Guardrail kalır, koşar; rol koşulu eklenir | Aynı | Zayıf kanıtla guardrail: yanlış pozitif ortak posta kutusu | Guardrail kalkar, **kullanıcı §5 kararı** |
| Maliyet | Orta | **Orta-düşük** | Yüksek | Düşük | Düşük, ama asıl tehdidi kapatmaz |

**Neden A değil A′.** Yeniden işe alım **yeni satırdır** (ADR 0010, `:191`; `00037:103-105` eski satırın adresini serbest
bırakır). Eski satırın işaretli kayıtları kuyrukta durabilir. `admin_users.employee_id` tek değer tutar; A′'de iki satır
aynı yöneticiyi gösterebilir. A′ ayrıca `admin_users`'ın daraltılmış yetki düzenine dokunmaz.

**Neden B değil.** Tek kazancı işletmeler arası tanımadır ve bu gerekmez: kayıt ve karar her zaman aynı tenant'tadır
(ADR 0027 K-1a; `transaction_reviews_txn_fk`). KM'de çalışan, yalnız KF'de yönetici olan kişi o kaydı zaten göremez.

**C'nin yeri:** kapı değil, **öneri**. Adres eşitliği sahibe "bu sen misin?" dedirtir, karar vermez.

## 4. Önerilen karar

Bağ:

- `employees.admin_user_id uuid NULL`, bileşik FK `(admin_user_id, tenant_id) → admin_users (id, tenant_id)`,
  `ON DELETE RESTRICT`. Çapraz tenant bağı FK reddeder (ölçüldü).
- Yalnız sahip yazar: Managers sayfası, `a.mayInvite` kapısı (`internal/handler/admininvites.go:168`), her bağ/çözme
  bir `audit_log` satırı. Tek yazan sqlc ifadesi tarama testiyle sabitlenir (`employees`'te tablo düzeyi `UPDATE` var).
- Adres eşleşiyorsa sahibe tek dokunuşla öneri çıkar. Otomatik bağ yok (soru S3).

Kural, üç kayıt yolunun hepsinde aynı:

| Karar veren | Kendi kaydı mı | Sonuç |
|---|---|---|
| Yönetici | Evet (bağlı) | **Reddedilir** (onay ve ret). Kayıt kuyrukta kalır, sahibe düşer. Deneme `audit_log`'a yazılır |
| Sahip | Evet (bağlı) | **İzin**, `self_decided: true` audit'te, raporda "kendi kaydı" etiketi |
| Herhangi biri | Hayır | Bugünkü gibi |
| Herhangi biri | Bilinmiyor (bağsız) | Bugünkü gibi. **Bu, kalan riskin kendisidir** (§8) |

- **`sys:no-self-review` guardrail kalır**, sayı 11 kalır. `Match` bağlı yönetici kimliğini ve `CtxActorRole`'ü okur; rol
  yoksa **reddeder** (fail-closed). `Context.SubjectEmployeeID` yeniden adlandırılır: alanın adı bu hatanın kendisidir.
- **İki katman:** alan paketleri (`review`, `claim`, `manual`) işlemin içinde bağı okuyup yalnız guardrail'ları koşar
  (`internal/domain/tenant/rulewriter.go:344-345` deseni), hata bir cümle olur ve deneme audit'e yazılır. Tetikleyici kemerdir.
  Tam kural kitabı çağrılmaz: M7-07'nin `deny sid=default` tuzağına düşer ve `base:authz-*` zaten iki role de izin verir.
- **Tetikleyiciler:** `tappa_check_transaction_review` ve `tappa_check_punch_claim_decision` değiştirilir; `self-review forbidden`
  öneki korunur (`internal/domain/review/review.go:417` onu okur). `transactions`'a `BEFORE INSERT … WHEN (NEW.channel = 'manual')`: tap yolu bu
  tetikleyiciyi hiç çalıştırmaz.
- **Elle giriş reddi nereye gider:** panel cümlesi sahibi ve telefondan saat talebini (M15) gösterir. Bu, Deputy'nin
  "güncelle ama onaylama" desenini bizim var olan talep akışıyla karşılar. Tap ekranı değişmez (§9).
- **İşletme ayarı yok:** sahip, yöneticisine öz-onay izni veremez. Bir müşteri isterse ayrı karardır.

**Damga neden `transaction_reviews`/`transactions` sütunu değil.** Bağ kişi hakkında bir olgudur, zaman hakkında değil.
Rapor etiketi `reviewer_id = e.admin_user_id` join'iyle türer: bağ kurulduğu gün **geçmiş öz-kararlar da görünür**, hiçbir
satır değişmez (§4.3). Prototipte bağdan önceki karar bu join'de çıktı. Bağ sonradan çözülürse etiket söner; bu yüzden karar
anındaki damga append-only `audit_log` `detail`'inde, kararla aynı işlemde durur. Bedeli: damgayı uygulama yazar, veritabanı
hesaplamaz. Sütun alternatifini inceleyiciler tartabilir.

## 5. Kırmızı çizgiler ve kişisel veri

- **§4.1, §4.2:** biyometri ve GPS yok, dokunulmaz.
- **§4.3:** `transactions`, `transaction_reviews`, `punch_claim_decisions` satırları ve sütunları değişmez. Yeni tetikleyiciler
  yalnız `INSERT`'i reddeder.
- **§4.5:** metin değişmez. A′ tenant içindedir, definer ve kapalı liste yok.
- **§4.6:** tap kaydı reddedilmez, kuyrukta kalır. Reddedilen elle giriş bir kayıt değil, bir beyandır; denemesi audit'te.
- **§5:** tablo ve `tap.Decide` değişmez. "Manuel kayıt" maddesine bir cümle eklenir: **kullanıcı onayı gerekir**.
- **GDPR:** yeni kişisel veri **kategorisi yok** ve kullanıcıdan yeni alan toplanmaz (`employees`'te telefon sütunu da yok, ölçüldü). Yeni olan
  tek şey, aynı işverendeki iki kaydın *aynı kişi* olduğu olgusudur. Bakış: yalnız o işletmenin panelinde. Operatör, holding ve
  akış yazarı okuyamaz (ölçüldü: `tappa_opdefiner`, `tappa_holdingdefiner`, `tappa_feedwriter`, sütun düzeyi). API, webhook ve akışa
  gitmez. Adres karşılaştırması bellekte yapılır, log'a yazılmaz (§7, ADR 0022 §10). Saklama `employees` satırının ömrünü izler.
  Bu not hukuki görüş değildir; gizlilik metninin güncellenmesi gerekip gerekmediği **bakılmadı**.

## 6. Uygulama planı

Yalnız genişletme; contract adımı yok. Migration Job rollout'tan önce koşar (ADR 0031 §2). Bütün bağlar NULL iken davranış
bugünküyle aynıdır, bu yüzden eski pod güvenle yaşar. Migration numarası uygulama anında bütün dallara bakılarak seçilir.

| Kart | İş | Dağıtım |
|---|---|---|
| **T148-0** | Rules ekranı metnini gerçeğe çek (`rulebook.go:863-864`, `:887`). Migration yok | **Karardan bağımsız, hemen çıkabilir** |
| **T148-1** | Sütun + FK (`lock_timeout`, `NOT VALID`, sonra `VALIDATE`), ayrı dosyada `CONCURRENTLY` indeks (`00070` deseni), iki tetikleyici + manuel tetikleyici, `EXECUTE` yalnız sahipte (`00061`), Down dolu, katalog testleri | 1. deploy, etki yok |
| **T148-2** | Guardrail: alan adı, rol koşulu, fail-closed, sebep cümlesi | T148-3 ile |
| **T148-3** | `review.Record`, `claim.Decide`, `manual`: bağı oku, guardrail koş, ret audit'i, `self_decided`. Ayrı cümleler (`internal/handler/review.go:475-482`, `internal/handler/claimqueue.go:479-481`). Kuyrukta kendi kaydında düğme yerine "sahip karar verecek" | 2. deploy, bağ olmadan etki yok |
| **T148-4** | Managers sayfası: bağ kur/çöz, adres önerisi, bağsız yönetici görünür. Askıda sıradan yazma gibi durur (ADR 0025). skill `tappa-brand` | 3. deploy, **koruma burada başlar** |
| **T148-5** | Rapor: docket etiketi, Roll toplamı, CSV'de ayrı alan; join `db/queries`'te | T148-4 ile ya da hemen sonra |
| **T148-6** | Dokümanlar: CLAUDE.md §5 (onaylı metin), ADR 0004 ve 0032 notları, `m3-policy-motoru.md:331`, `:355`, `open-questions.md:51`, kod yorumları, T148 kapanışı. `/security` iddiası: *"Nobody can decide"* yasağı kalır (sahip istisnası yüzünden hâlâ yanlış) | Son |

**Kabul kriterleri**

1. Bağlı yönetici kendi kaydında üç yolda da reddedilir: alan katmanı **ve** alan katmanı atlanınca tetikleyici.
2. Sahip kendi kaydında üç yolda da geçer; audit'te `self_decided: true`, raporda etiket.
3. Bağsız bir hesabın davranışı bugünküyle bayt bayt aynıdır.
4. Yalnız sahip bağ yazar; yönetici 403 alır ve deneme audit'e düşer. Çapraz tenant bağı FK ile reddedilir.
5. `transactions`, `transaction_reviews`, `punch_claim_decisions` üzerinde `UPDATE`/`DELETE` yok; geçmiş bir satır değişmeden öz-karar olarak okunur.
6. Rules ekranı metni, davranışla aynıdır.
7. Tap ve sonuç ekranları, `tap.Decide` testleri, public iddia pinleri değişmez.

**Test tablosu.** Yeni testlerin adını yapıcı koyar; §5 tablosuna satır eklenmediği için yeni `tap.Decide` vakası yoktur.

| Katman | Vaka | Beklenen |
|---|---|---|
| `internal/policy` (tablo) | yönetici/kendi · sahip/kendi · rol yok/kendi · farklı kişi · bağ yok | deny · yok · deny · yok · yok |
| `internal/db` (gerçek Postgres, `tappa_app`) | Tetikleyici: üç yol × iki rol × bağlı/bağsız; `approved` ve `rejected` ayrı | Matrisle aynı, `self-review forbidden` öneki |
| `internal/db` | RLS: A'nın bağlantısı B'nin bağını görmez (`tenant_id` filtresiz); çapraz FK reddi; tap kanalında manuel tetikleyici çalışmaz | 0 satır · hata · tetiklenmez |
| `internal/db` | Yetki: yeni sütun operatör, holding, akış rolünde okunamaz | `has_column_privilege` false |
| `internal/domain/review`, `claim`, `manual` | Ret bir satır audit yazar, kayıt yazmaz; sahip kararı damgalı | Satır sayısı ölçülür |
| `internal/handler` | Sahip kapısı, ret cümlesi, kuyrukta düğme yok, öneri adresi log'a düşmez | Gerçek HTTP |
| `internal/domain/ledger` | Etiket, bağdan önceki karar dahil; toplam değişmez | Etiket var, saat aynı |

Değişecek var olan testler: `TestDecideDB_TheSelfDecisionCheckFiresOnlyOnAForgedIdentity` (sahte satır yerine gerçek bağ),
`internal/db/triggerexecute_test.go:142-145`, `TestGuardrails_SelfReviewOnlyWhenSamePerson`,
`TestGuardrailGuards_ReCheckedWhenTheShippedSetChanges`. Yeşil kalması gerekenler: `TestPanel_CallsThePolicyEngineNowhere`,
`TestManualEntryAction_HitsNoGuardrailAndIsGrantedToBothPanelRoles` (çift dolu değilken), `TestDecide_DirectionNilForNonRecordVerdicts`
ve `tap.Decide` ailesinin tamamı, `TestEndpointState_NeverPaysAVerdictItDoesNotKNOW`, `TestAuditLog_IsAppendOnlyForTheApp`,
`TestRLS_PunchClaims_OwnerMutationsHitAppendOnlyTrigger`, `TestSecurityPage_SaysNoneOfTheRefusedPhrasings`.

## 7. Prototip

Geçici Postgres, goose 87 + T148-1'in taslak SQL'i, `tappa_app`, 2026-11-06; depoya girmedi, veritabanı silindi.

| Durum | Sonuç |
|---|---|
| Bağsız yönetici, kendi kaydı | Yazıldı (bugünkü boşluk) |
| Bağlı yönetici: onay, ret, kendi manueli | Üçü de reddedildi |
| Sahip: yöneticinin kaydı, kendi kaydı, kendi manueli | Üçü de yazıldı |
| Yönetici, başkasının kaydı | Yazıldı |
| Başka tenant'ın yöneticisine bağ | FK reddetti |
| Öz-karar join'i | 3 satır: bağdan önceki iki yönetici kararı ve sahibin kararı |

Ölçülmedi: `punch_claim_decisions` tetikleyicisinin yeni hali, guardrail değişikliği, ekranlar.

## 8. Kalan risk

- **Eşlenmemiş hesap, en büyüğü.** Sahip bağ kurmazsa koruma o hesapta yoktur. Damga da yoktur. T148-4 yalnız görünür yapar.
- **Hesap paylaşımı.** Yönetici sahibin girişiyle onaylarsa kayıt sahibin adına yazılır. Hiçbir seçenek kapatmaz.
- **İkinci çalışan satırı (Y-D).** Yönetici kendine bağsız bir profil açabilir. Kabul edilmiş risk sürer; M6-11 sinyalleri izler.
- **Yokluktaki sahip.** Yöneticinin işaretli saati sahip bakana dek saate girmez. Bu niyetlenen bedeldir; talep gelirse izin ayrı karardır.
- **`reviewer_id` FK'siz** (`00006:217-223`). Üretim yolu oturumdan yazar, ama sahte bir id bağla eşleşmez. Bu ADR'nin dışında; ayrı backlog adayı.
- **İkinci sahip.** Sahip damgayla geçer; öteki sahip raporda görür, engel olmaz.

## 9. Kullanıcıya sorular

**S1. Sahip kendi kaydına karar verebilsin mi?**
- **(a) Evet, damgalı ve raporda görünür. Öneri.** Piyasayla uyumlu, tek sahipte çıkmaz yok, kilit ve sayım yok.
- (b) Yalnız başka aktif sahip yoksa. Daha sıkı; ama reddedilmiş üyeliği ayıran `unlinked_at`'i `tappa_app` okuyamaz (§4.5). Sayım için `resolve_admin_owners_by_tenant` gerekir; tetikleyiciden kullanılabilirliği ölçülmedi.
- (c) Hayır, istisna yok. Y-C'nin lafzı korunur; tek sahipli işletmede kayıt çözülemez kalır.

**S2. Yönetici kendi saatini elle girebilsin mi?**
- **(a) Hayır; sahip girer ya da yönetici telefondan saat talebi açar. Öneri.** Var olan akış, yeni kayıt türü yok.
- (b) Girer ama `flag` olur, sahibe düşer. Piyasadaki "güncelle, onaylama" desenine en yakın; `InsertManualTransaction`'ın sabit `'ok'`'unu ve onun ardındaki gerekçeyi değiştirir.
- (c) Girer, yalnız görünür olur. Ucuz; ama iki onay yolunu kapatmanın anlamı kalmaz (§1.2).

**S3. Var olan hesaplar nasıl bağlansın?**
- **(a) Sahip bağlar, adres eşleşince tek dokunuş öneri. Öneri**, §10'daki Q2 sonucuna bakarak kesinleşir.
- (b) Migration aynı adreslileri kendisi bağlar, sahibe bildirir. Koruma ilk gün başlar; ama çalışan ekleyen her yönetici adresi serbest yazar (`internal/handler/employeeemail.go:34-35`), yani kişi hakkında kimsenin onaylamadığı bir olgu yazılmış olur.
- (c) Öneri bile yok, yalnız elle. En sade, en çok unutulan.

## 10. Açık bilgi ve canlı sorgular

**Ölçülmedi:** canlıda kaç işletmede yönetici var; gerçekten öz-onay olmuş mu; canlı tablolarda `ALTER` kilit süresi; When I Work'ün üst rolleri, Homebase, ADP ve Gusto'nun kendi kart davranışı (§2).

⚠️ Sayıyı yorumlamadan önce Q2'de **hangi işletmeler** olduğuna bakın (KF/KM ve deneme işletmeleri).

Yalnız sayı döner, adres dönmez (Q2 işletme adını ve sayıları döndürür). `BEGIN READ ONLY` içinde, RLS'siz okuyabilen rolle. Goose 87 şemasında çalıştırılarak denendi.

```sql
-- Q1: işletmelerin yetkili dağılımı (kaçında tek yetkili var)
SELECT owners, managers, count(*) AS businesses FROM (
  SELECT tenant_id, count(*) FILTER (WHERE role='owner') AS owners,
         count(*) FILTER (WHERE role='manager') AS managers
  FROM admin_users WHERE status='active' AND unlinked_at IS NULL GROUP BY tenant_id) s
GROUP BY owners, managers ORDER BY owners, managers;

-- Q2: işletme başına: yetkili sayısı, kadro, yetkiliyle aynı adresli ve yalnız aynı adlı çalışan
SELECT t.name AS business,
  count(DISTINCT a.id) FILTER (WHERE a.role='owner')   AS owners,
  count(DISTINCT a.id) FILTER (WHERE a.role='manager') AS managers,
  (SELECT count(*) FROM employees e WHERE e.tenant_id=t.id AND e.status<>'deactivated') AS staff,
  count(DISTINCT e2.id) AS staff_with_an_admins_address,
  count(DISTINCT e3.id) AS staff_with_an_admins_name_only
FROM tenants t
JOIN admin_users a ON a.tenant_id=t.id AND a.status='active' AND a.unlinked_at IS NULL
LEFT JOIN employees e2 ON e2.tenant_id=t.id AND e2.email=a.email AND e2.status<>'deactivated'
LEFT JOIN employees e3 ON e3.tenant_id=t.id AND e3.status<>'deactivated'
     AND lower(btrim(e3.full_name))=lower(btrim(a.full_name))
     AND e3.email IS DISTINCT FROM a.email
GROUP BY t.id, t.name ORDER BY t.name;

-- Q3: adrese göre geçmiş öz-kararlar (alt sınır: farklı adresliyi kaçırır; rol bugünkü)
SELECT 'transaction_reviews' AS path, a.role AS role_today, count(*) AS self_by_address
FROM transaction_reviews r
JOIN transactions t ON t.id=r.transaction_id AND t.tenant_id=r.tenant_id
JOIN employees e ON e.id=t.employee_id AND e.tenant_id=t.tenant_id
JOIN admin_users a ON a.id=r.reviewer_id AND a.tenant_id=r.tenant_id AND a.email=e.email
GROUP BY a.role
UNION ALL
SELECT 'punch_claim_decisions', a.role, count(*)
FROM punch_claim_decisions d
JOIN punch_claims c ON c.id=d.claim_id AND c.tenant_id=d.tenant_id
JOIN employees e ON e.id=c.employee_id AND e.tenant_id=c.tenant_id
JOIN admin_users a ON a.id=d.decided_by AND a.tenant_id=d.tenant_id AND a.email=e.email
GROUP BY a.role
UNION ALL
SELECT 'manual_transactions', a.role, count(*)
FROM transactions t
JOIN employees e ON e.id=t.employee_id AND e.tenant_id=t.tenant_id
JOIN admin_users a ON a.id=t.entered_by AND a.tenant_id=t.tenant_id AND a.email=e.email
WHERE t.channel='manual'
GROUP BY a.role
ORDER BY 1, 2;

-- Q4: Q3'ün paydası
SELECT (SELECT count(*) FROM transaction_reviews)   AS reviews,
       (SELECT count(*) FROM punch_claim_decisions) AS claim_decisions,
       (SELECT count(*) FROM transactions WHERE channel='manual') AS manual_rows,
       (SELECT count(*) FROM transactions t WHERE t.verdict='flag' AND NOT EXISTS (
            SELECT 1 FROM transaction_reviews r WHERE r.transaction_id=t.id)) AS flags_awaiting;

-- Q5: şu an bekleyen ve bir yetkilinin kendi kaydı olan işaretli satırlar (kural açılınca etkilenenler)
SELECT count(*) AS flags_awaiting_on_an_admins_own_row
FROM transactions t
JOIN employees e ON e.id=t.employee_id AND e.tenant_id=t.tenant_id
JOIN admin_users a ON a.tenant_id=t.tenant_id AND a.email=e.email AND a.status='active'
WHERE t.verdict='flag' AND NOT EXISTS (SELECT 1 FROM transaction_reviews r WHERE r.transaction_id=t.id);
```
