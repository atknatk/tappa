# M1 — Veri katmanı

**Amaç.** [docs/handoff.md](../handoff.md) §7 şemasını, her tabloda tenant
izolasyonu ve `transactions` değişmezliği mekanik olarak zorlanmış biçimde
kurmak; üzerine tip güvenli erişim katmanı ve gerçek demo veriyi koymak.

**Bittiğinde:** 10 tablo + RLS, tenant kapsamlı transaction yardımcısı, sqlc
üretimi, çalışan seed, ve *A tenant B'nin satırını göremez* diye kanıtlayan test.

**Ana araç:** agent `tappa-db-migrator` — her tablo görevinde kullan; migration,
RLS, GRANT, sqlc ve testi birlikte teslim eder.

> **Her tablonun beş zorunlu unsuru:** `tenant_id uuid NOT NULL` · `tenant_id`
> indeksi · `ENABLE` + `FORCE ROW LEVEL SECURITY` · hem `USING` hem `WITH CHECK`
> politikası · `tappa_app` GRANT'i. Biri eksikse migration eksiktir ve
> `scripts/redline-check.sh` R5 bunu yakalar.

---

## M1-01 — ADR 0002: tenant bağlamı ve RLS stratejisi

- **Bağımlılık:** M0-03
- **Kırmızı çizgi:** §4.5
- **Commit:** `docs(adr): record tenant context and RLS strategy`

**Amaç.** Tenant bağlamının nasıl kurulacağını, kimin hangi rolle bağlandığını ve
"kuşak + kemer" yaklaşımını yazılı karara bağlamak.

**Neden.** Bundan sonraki her migration ve her sorgu bu karara uyacak. Karar
yazılı değilse sıfır hafızalı ajan kendi yorumunu üretir ve izolasyon sessizce
delinir.

**Dokunulacak dosyalar.** `docs/adr/0002-tenant-baglami-ve-rls.md`

**İçermesi gerekenler.**
1. Uygulama `tappa_app` ile bağlanır (NOBYPASSRLS, tablo sahibi değil); migration
   `tappa_owner` ile. Config bunu zaten zorluyor.
2. Bağlam **transaction başına** `SET LOCAL app.tenant_id = $1` ile verilir.
   Havuzdan alınan bağlantıda `LOCAL`'siz `SET` **yasak** — bağlantı havuza kirli
   döner ve bir sonraki tenant onun bağlamıyla sorgu koşar.
3. Politikalar `app.tenant_id`'yi okur ve bağlam yokken **fail-closed** davranır:
   politika hiçbir satırı eşleştirmez. **Bu davranış test edilir** —
   [M1-09](#m1-09--rls-izolasyonu-ve-değişmezlik-testleri) vaka 3. Politikanın
   yazılacağı **tam ifade** [Q27](open-questions.md)'ye bağlıdır (çıplak cast mı,
   `NULLIF`'li mi); ADR bunu karara bağlamadan hiçbir migration yazılmamalı, bkz.
   aşağıdaki kart düzeltmesi.
4. Sorgularda ayrıca **açık** `tenant_id = @tenant_id` filtresi yazılır. RLS'in
   kurulmadığı bir kod yolunda tek savunma budur.
5. Tenant'ın kendisi (`tenants` tablosu) `id` üzerinden aynı politikayı alır.
6. Süper-admin / çapraz-tenant erişim ihtiyacı: **MVP'de yok**. Gerekirse ayrı
   bir rol ve ayrı bir ADR ile gelir; `tappa_app`'e BYPASSRLS asla verilmez.
7. **Tenant çözümleme istisnası — bu ADR'nin en kritik maddesi.** Bir tap
   geldiğinde elimizde yalnız tag UID (URL'den) ve oturum çerezi var; **ikisi de
   tenant taşımıyor**. `app.tenant_id` tam da bu iki aramanın *sonucu*. Yani
   `GetTagByUID` ve `GetEmployeeBySessionHash` çalıştığı anda tenant bağlamı
   kurulamaz — "her erişim `WithTenant` üzerinden" kuralıyla "her sorguda açık
   `tenant_id` filtresi" kuralı bu iki sorgu için aynı anda sağlanamaz.
   ADR bu istisnayı **açıkça, dar ve testli** tanımlamalı:
   - Yalnız bu iki sorgu, ayrı ve adlandırılmış bir "tenant çözümleme" yolunda.
   - `tappa_app` rolüyle, ama `tenant_id` filtresi olmadan → dolayısıyla bu iki
     tablo bağlam yokken **yalnız bu anahtar aramalarına** izin vermeli —
     **genel bir bypass açılmaz**. ⚠️ Bunun **saf RLS ile yazılamayacağına**
     dikkat: RLS satır bazlı bir boolean'dır, sorgunun `WHERE` şeklini göremez;
     gereken çevrelenmiş (bounded) bir bypass yüzeyidir. Tam sınır — beş arayüz
     kısıtı, definer superuser olamaz ve §6 FORCE altında yalnız `BYPASSRLS` —
     [ADR 0002 madde 7](../adr/0002-tenant-baglami-ve-rls.md)'de (aşağıdaki
     2026-07-25 kart düzeltmesi).
   - Çözümlemenin ardından bağlam kurulur ve **geri kalan her şey** `WithTenant`
     içinde koşar.
   - `sys:tenant-mismatch` guardrail'i ([M3-05](m3-policy-motoru.md)) etiketin
     tenant'ı ile oturumun tenant'ı ayrıştığında akışı keser.
   Bu istisna yazılmazsa uygulama anında ya ayrıcalıklı bir rol eklenir
   (NOBYPASSRLS kuralı fiilen delinir) ya da tap akışı hiç çalışmaz ve
   geliştirici tenant izolasyonundaki tek deliği **tam da kimlik doğrulama
   yolunda** açar.

**Kabul kriterleri.**
- ADR yazıldı, "kabul edildi" durumunda, tarihli.
- Alternatifler (şema-per-tenant, yalnız uygulama filtresi) ve neden seçilmedikleri var.
- **ADR [Q27](open-questions.md)'yi karara bağlar:** politika ifadesi çıplak cast
  mı `NULLIF`'li mi? Karar yazılmadan M1-02 ve sonrası **başlamaz** (aşağıdaki
  kart düzeltmesindeki ölçüme dayanır).
- M1-02 bu ADR'ye referansla yazılabiliyor.

> **Kart düzeltmesi (2026-07-24, M0-03 uygulaması sırasında).** Madde 3'ün eski
> hâli ölçümle **çürütüldü** ve yeniden yazıldı. Eskisi şuydu: *"Politikalar
> `current_setting('app.tenant_id', true)::uuid` okur. İkinci parametre `true`:
> ayar yoksa hata değil `NULL` döner → politika hiçbir satırı eşleştirmez →
> **fail-closed**. Bu davranış test edilir."*
>
> Sonuç kısmı (**fail-closed** ve *"bu davranış test edilir"*) **doğru ve
> korundu**; çürüyen şey `NULL` gerekçesidir. Ölçüm (`tappa_app`, canlı sonda):
>
> | bağlantının durumu | `current_setting('app.tenant_id', true)` | çıplak `::uuid` |
> |---|---|---|
> | GUC'a **hiç** yazılmamış | `NULL` | `NULL` → 0 satır ✅ |
> | GUC'a bir kez **yazılmış**, tx bitmiş | `''` | **`ERROR: invalid input syntax for type uuid: ""`** |
>
> Tetikleyici bağlantının kaçıncı kullanımı değil, GUC'a **ilk yazma**dır: taze
> bir bağlantıda arka arkaya üç bağlamsız sorgu koşuldu → üçünde de `NULL`, 0
> satır, hata yok. `''`'e düştükten sonra `ROLLBACK`, `RESET app.tenant_id` ve
> **`DISCARD ALL`** de `NULL`'a döndürmüyor (üçü ayrı ayrı ölçüldü); tek yol
> **yeni bağlantı**. Havuzda (`pgxpool`) bu, ilk `SET LOCAL`'dan sonra o bağlantı
> için **kalıcı** durumdur.
>
> **İkisi de fail-closed** (ne satır sızar ne sessiz onay — §4.6 korunuyor);
> kırılan şey güvenlik değil **determinizm**: aynı hata (unutulmuş `SET LOCAL`)
> bağlantı geçmişine göre iki farklı davranış üretir.
>
> Karara bağlanması gereken, politikanın **tam ifadesidir** — M0-03 bunu
> bilinçli olarak **karara bağlamadı**, yalnız ölçtü; orkestratör
> [Q27](open-questions.md) olarak açtı:
> - **(a) çıplak** `current_setting('app.tenant_id', true)::uuid` — eksik bağlam
>   ilk yazmadan sonra **yüksek sesle patlar**; savunulabilir, ama davranış
>   bağlantı geçmişine bağlı.
> - **(b)** `NULLIF(current_setting('app.tenant_id', true), '')::uuid` — her iki
>   durumda da `NULL`, yani **her zaman** 0 satır. Uçtan uca doğrulandı: aynı
>   bağlantıda tenant A → 1 satır, commit sonrası → 0 satır, tenant B → 1 satır,
>   hata yok.
>
> Q27 ne karara bağlarsa **CLAUDE.md §6'daki ifade, M1-02 adım 3 ve M1-09 vaka 3
> aynı biçimi göstermek zorundadır**; bugün üçü tutarlı değil. (CLAUDE.md'yi
> güncellemek **orkestratörün** işidir — M0-03 ona dokunmadı.)

> **Kart düzeltmesi (2026-07-25, M1-01 uygulaması sırasında).** Madde 7'nin
> "ör. `tag_uid`/`token_hash` eşitliğine dayalı, **sütun bazında kısıtlı
> politika**" örneği ADR 0002 tarafından **çürütüldü**. Saf RLS satır bazlı bir
> boolean'dır ve sorgunun `WHERE` şeklini göremez; dolayısıyla "bağlam yokken
> yalnız anahtar aramalarına izin ver ama `SELECT *`'a verme" **bir RLS
> politikasıyla ifade edilemez** — asıl gereken çevrelenmiş (bounded) bir bypass
> yüzeyidir. Bağlayıcı şart **korundu** (**genel bir bypass açılmaz**; dar,
> adlandırılmış, testli); düzeltilen yalnız yanlışlanan örnektir. Doğru mekanizma
> [ADR 0002 madde 7](../adr/0002-tenant-baglami-ve-rls.md)'de normatif: beş
> arayüz kısıtı (anahtar girdi · en çok 1 satır · `SELECT *` yüzeyi yok ·
> `tappa_app`'e yalnız `EXECUTE` · naif "bağlam NULL iken satır" RLS dalı yasak)
> \+ definer **superuser olamaz**. §6 her tabloya `FORCE` zorunlu kıldığı için
> çevrelenmiş bypass **yalnız `BYPASSRLS`** olabilir — salt-`SELECT` bir rol
> bağlam yokken 0 satır görür (FORCE'suz-sahip yolu §6 altında kapalı).

---

## M1-02 — Migration 0001: tenants

- **Bağımlılık:** M1-01
- **Kırmızı çizgi:** §4.5 · §6
- **Araç:** agent `tappa-db-migrator`
- **Commit:** `feat(db): add tenants table with RLS`

**Amaç.** Hiyerarşinin kökünü kurmak.

**Alanlar** (handoff §7): `id`, `name`, `vat_number`, `business_type`,
`structure` (`single|multi`), `plan`, `created_at`. Q01 cevabına göre `timezone`.

**Adımlar.**
1. `make migrate-new name=create_tenants`
2. Tabloyu yaz; `structure` ve `business_type` için `CHECK` kısıtı veya enum.
3. RLS: politika `id = NULLIF(current_setting('app.tenant_id', true), '')::uuid`
   (bu tabloda `tenant_id` yerine `id`). Biçim [Q27](open-questions.md) ile
   `NULLIF`'e karara bağlandı ve [ADR 0002](../adr/0002-tenant-baglami-ve-rls.md)
   madde 3'te normatiftir — **çıplak cast YASAK**; iki biçim repoda karışık kalırsa
   politikalar tablodan tabloya farklı davranır. Hem `USING` hem `WITH CHECK` yazılır.
4. `GRANT` + indeks + dolu `-- +goose Down`.
5. `make migrate` → `make migrate-down` → `make migrate` (üçü de temiz).

**Kabul kriterleri.**
- RLS beşlisi tam; `redline-check.sh` R5 temiz.
- `Down` gerçekten çalışıyor.
- `vat_number` tenant içinde tekil; format doğrulaması **uygulama** katmanında
  (DB'ye VIES bilgisi girmez).

**Tuzaklar.**
- `tenants` üzerinde politika kurmayı unutmak klasik hata: tenant listesi
  sızarsa müşteri isimleri ve VAT numaraları görünür.
- Q01 cevaplanmadıysa `timezone` alanını **atlama** — sonradan eklemek
  `locations` vardiya yorumunu geriye dönük bozar. Cevap yoksa görevi beklet.

---

## M1-03 — Migration 0002: locations & departments

- **Bağımlılık:** M1-02 · Q01 · Q07
- **Kırmızı çizgi:** §4.5 · §6
- **Araç:** agent `tappa-db-migrator`
- **Commit:** `feat(db): add locations and departments`

**Amaç.** *Proof of place*'in ve vardiya çözümünün veri temeli.

**Alanlar.**
`locations(id, tenant_id, name, static_ips, gps_lat, gps_lng, shift_start,
shift_end, overnight, created_at)` ·
`departments(id, tenant_id, location_id, name, shift_start, shift_end, overnight,
created_at)`

**Kararlar.**
- `static_ips`: Q07 → `inet[]` (tek IP = /32) veya `cidr[]`.
- `gps_lat`/`gps_lng`: `numeric(9,6)` — **float değil** (§6).
- `shift_start`/`shift_end`: `time`; `overnight bool` bitişin ertesi güne
  sarktığını söyler (Rusty Bar 18:00–02:00).
- `departments.shift_*` **nullable**: doluysa lokasyon vardiyasını ezer (§5).

**Kabul kriterleri.**
- Her iki tabloda RLS beşlisi tam.
- `departments.location_id` → `locations` FK, ikisi de aynı `tenant_id`'de
  (FK'ye ek olarak bileşik anahtar veya CHECK ile çapraz-tenant bağlanma engellenir).
- `overnight = true` olan bir kayıt yazılıp okunabiliyor.

**Tuzaklar.**
- FK tek başına çapraz-tenant referansı engellemez: A tenant'ının departmanı
  B tenant'ının lokasyonuna bağlanabilir. `UNIQUE (id, tenant_id)` + bileşik FK
  ile kapat.
- `time` tipinde vardiya tutmak doğru; **`timestamp` kullanma**. Ama karşılaştırma
  yaparken tarih + TZ birleştirme render/domain katmanında yapılır.

---

## M1-04 — Migration 0003: employees & sessions

- **Bağımlılık:** M1-03
- **Kırmızı çizgi:** §4.1 (biyometri yok) · §4.5 · §4.7 (token değil hash)
- **Araç:** agent `tappa-db-migrator`
- **Commit:** `feat(db): add employees and sessions`

**Amaç.** *Proof of person*'un veri temeli.

**Alanlar.**
`employees(id, tenant_id, location_id, department_id, full_name, role, email,
status, invited_at, activated_at, deactivated_at, created_at)` —
`status: invited|active|deactivated` ·
`sessions(id, tenant_id, employee_id, token_hash, device_info, created_at,
last_used_at, revoked_at)`

**Kabul kriterleri.**
- `sessions.token_hash` saklanır, **token asla**. Sütun adı bunu açıkça söyler.
- `token_hash` üzerinde UNIQUE indeks (doğrulama tek sorguda).
- `sessions` tablosunda da `tenant_id` + RLS var (handoff §7'de yok — **bilinçli
  ekleme**, CLAUDE.md §6 "her tabloda tenant_id" diyor).
- Hiçbir alan biyometrik veri taşımıyor; `device_info` yalnız kaba cihaz etiketi
  (model/tarayıcı), parmak izi/kimlik değil.
- Silme yok: iptal `revoked_at` damgasıdır.

**Tuzaklar.**
- `device_info`'yu tarayıcı fingerprint'ine dönüştürme — §4.1'in ruhuna aykırı ve
  denetimde bulgu olur.
- `email` `citext` olsun (uzantı zaten yüklü); büyük/küçük harf farkıyla ikinci
  davet gönderilmesin.

---

## M1-05 — Migration 0004: tags

- **Bağımlılık:** M1-03
- **Kırmızı çizgi:** §4.4 (atomik ctr) · §4.7 (anahtar sarmalı) · §4.5
- **Araç:** agent `tappa-db-migrator` + skill `tappa-sun`
- **Commit:** `feat(db): add tags table with monotonic counter`

**Amaç.** Plaket kaydı ve replay korumasının **durum** tarafı.

**Alanlar.** `tags(uid, tenant_id, location_id, aes_key_ref, last_ctr, status,
created_at, retired_at, replaced_by)` — `status: active|retired|lost`

**Kararlar.**
- `uid` birincil anahtar: 7 byte UID'nin 14 haneli hex hali (`char(14)` veya
  `bytea`). Seçimi kartta gerekçelendir; sorgu ve log okunabilirliği için hex
  metin tercih edilir.
- `last_ctr`: `integer` (24 bit sayaç sığar) `NOT NULL DEFAULT 0`.
- `aes_key_ref`: KEK ile **sarmalanmış** anahtar (`bytea`). Düz anahtar asla.
- `replaced_by`: "Replace tag" akışında yeni UID'ye işaret eder (audit izi).

**Kabul kriterleri.**
- RLS beşlisi tam.
- Atomik ilerletme sorgusu M1-08'de tanımlı ve **tek ifade**:
  `UPDATE tags SET last_ctr = @ctr WHERE uid = @uid AND last_ctr < @ctr RETURNING uid`
- Eski etiket **silinmiyor**: `retired_at` + `status` ile emekli ediliyor;
  geçmiş `transactions.tag_uid` hâlâ çözülebiliyor.

**Tuzaklar.**
- `last_ctr` karşılaştırması `>=` **değil** `<` olmalı — `>=` aynı sayacı iki kez
  geçirir, yani replay'in ta kendisi.
- `tags` üzerinde FK'yi `transactions.tag_uid`'den `ON DELETE CASCADE` ile kurma:
  etiket kaydı silinirse mesai geçmişi gider. Zaten silme yok, ama kısıt
  `RESTRICT` olsun.

---

## M1-06 — Migration 0005: transactions (append-only) & audit_log

- **Bağımlılık:** M1-04 · M1-05
- **Kırmızı çizgi:** §4.3 (immutable) · §4.6 (kayıt kaybolmaz) · §4.5
- **Araç:** agent `tappa-db-migrator`
- **Commit:** `feat(db): add append-only transactions and audit_log`

**Amaç.** Ürünün asıl kaydı. Hukuki delil olabilecek, **asla değişmeyen** tablo.

**Alanlar** (handoff §7 + denetim eklemeleri): `id, tenant_id, employee_id,
location_id, department_id, tag_uid, ctr, type (in|out|null), occurred_at,
source_ip, ip_match, gps_lat, gps_lng, gps_match, sun_valid, trust,
verdict (ok|flag|reject|ignored), note, channel (nfc|qr|manual), entered_by,
practice, **queued**, created_at` ·
`audit_log(id, tenant_id, actor_id, action, target, detail, at)` ·
`transaction_reviews(id, tenant_id, transaction_id, reviewer_id, outcome,
             note, reviewed_at)`  — `outcome: approved|rejected`

**`employee_id`, `location_id`, `department_id` NULLABLE olmak zorunda.**
§5'te satır 1 (etiket `lost`/`retired`) ve satır 2 (SUN geçersiz), satır 3'ten
(oturum yok) **önce** gelir. Yani çerezi olmayan biri çalınmış bir plakete
dokunduğunda `employee_id` olmayan bir `reject` kaydı yazılmak **zorunda** —
ve bu, kayıt tutmayı en çok istediğimiz senaryodur. `NOT NULL` varsayımıyla
yazılan migration'da bu INSERT patlar, hata yoluna düşer ve kayıt kaybolur:
§4.6'nın tam da var oluş sebebi olan durumda §4.6 ihlali. Hangi `verdict`
değerlerinde hangi alanların null olabileceği CHECK kısıtıyla sabitlenir.

**Değişmezliğin mekanik zorlanması.**
1. `REVOKE UPDATE, DELETE ON transactions FROM tappa_app;` (GRANT'ı yalnız
   `SELECT, INSERT` ver).
2. Ek olarak `BEFORE UPDATE OR DELETE` trigger'ı `RAISE EXCEPTION` atsın —
   `tappa_owner` ile bağlanan bir yolu da kapatır (kuşak + kemer).
3. `audit_log` da append-only aynı yöntemle.

**Kabul kriterleri.**
- `tappa_app` ile `UPDATE transactions …` **hata** veriyor (test M1-09'da).
- `tappa_owner` ile `DELETE FROM transactions …` de trigger'a takılıyor.
- İndeksler: `(tenant_id, occurred_at DESC)`, `(tenant_id, employee_id,
  occurred_at DESC)` — ikincisi yön tayini (son açık giriş) sorgusunun temeli.
- `verdict`, `channel`, `type` için CHECK kısıtı; `trust` `smallint`.
- FLAGGED onay akışı `transactions`'a **hiç dokunmaz** (Q20 kararı): onay/ret
  `transaction_reviews`'a yazılır + `audit_log`. `transactions` yalnız tap
  kaydıdır — anlamı net kalır, saat toplamı şişmez. M6-04 uygular.
- `transaction_reviews` de append-only (aynı REVOKE + trigger kalıbı) ve RLS
  beşlisi tam. `transaction_id` üzerinde indeks; rapor son onayı JOIN ile okur.
- **`transaction_reviews` üç kısıtla doğar** — yoksa Q20, `transactions`'ı
  koruyup etkin durumu sınırsız değiştirilebilir kılar (§4.3'ün lafzı korunur,
  ruhu yok olur):
  1. `UNIQUE (transaction_id)` — bir kayıt **bir kez** karara bağlanır. Aksi
     hâlde müdür `approved`/`rejected`/`approved` yazarak etkin sonucu istediği
     kadar değiştirir.
  2. `transaction_id` yalnız `verdict = 'flag'` kayıtlara işaret edebilir
     (CHECK/trigger). Aksi hâlde müdür trust 100, `ok` bir kayda `rejected`
     yazar ve **saati siler** — `transactions` üzerinde UPDATE yetkisi
     olmamasına rağmen.
  3. `reviewer_id <> transactions.employee_id` — kendini onaylama yasak;
     guardrail karşılığı `sys:no-self-review` ([M3-05](m3-policy-motoru.md)).

**Tuzaklar.**
- **`(tag_uid, ctr)` üzerine UNIQUE kısıt KOYMA.** Cazip görünür ("replay'i DB
  engellesin") ama reddedilen bir replay denemesi de kayıt olarak yazılır (§4.6)
  ve aynı `(tag, ctr)` çiftini taşır → UNIQUE ihlali → kayıt kaybı. Replay
  koruması **yalnızca** `tags.last_ctr` atomik güncellemesindedir.
- `source_ip` `inet` tipinde; log'a tam GPS yazılmaz ama DB'ye yazılır (kayıt
  bütünlüğü) — bu ayrımı karıştırma.
- `occurred_at` ile `created_at` farklıdır: ilki tap anı (çevrimdışı kuyrukta
  geçmiş olabilir), ikincisi satırın yazıldığı an.

---

## M1-07 — internal/db: havuz ve tenant kapsamlı transaction

- **Bağımlılık:** M1-02
- **Kırmızı çizgi:** §4.5 · §6 (`SET LOCAL`)
- **Commit:** `feat(db): add pgx pool and tenant-scoped transaction helper`

**Amaç.** Her sorgunun doğru tenant bağlamında koşmasını **yapısal olarak**
garanti eden tek giriş noktası.

**Neden.** Bağlam kurmayı çağrı yerlerine bırakırsak, bir yerde unutulur.
Unutulduğunda RLS fail-closed davranır (0 satır) — sessiz veri kaybı gibi görünür,
teşhisi zordur.

**Dokunulacak dosyalar.** `internal/db/pool.go`, `internal/db/tenant.go`

**Tasarım.**
```go
// WithTenant bir transaction açar, tenant baglamini SET LOCAL ile kurar ve
// fn'i o transaction icinde calistirir. SET LOCAL transaction disina sizmaz.
func (d *DB) WithTenant(ctx context.Context, tenantID uuid.UUID,
    fn func(context.Context, store.Querier) error) error
```
- `SET LOCAL app.tenant_id = $1` — **asla** `LOCAL`'siz.
- Hata → rollback; başarı → commit; panik → rollback + yeniden panik.
- Havuz ayarları config'ten; `pgx.Pool` tek yerde kurulur.

**Kabul kriterleri.**
- `SET` (LOCAL'siz) hiçbir yerde yok (`redline-check.sh` R5 uyarısı temiz).
- Bağlantı havuza döndükten sonra `current_setting('app.tenant_id', true)`
  boş — test ile kanıtlanır.
- Handler'lar doğrudan `pgxpool` görmez; yalnızca bu yardımcıyı kullanır.

**Tuzaklar.**
- `SET LOCAL` parametre bağlama (`$1`) kabul etmez; `set_config('app.tenant_id',
  $1, true)` kullan — üçüncü argüman `true` = transaction-local. String
  birleştirme ile SQL yazma.
- Tenant'ı `context.Context` içinde taşımak cazip; taşı ama **tek okuma yeri**
  bu yardımcı olsun.

---

## M1-08 — İlk sqlc sorguları

- **Bağımlılık:** M1-06 · M1-07
- **Kırmızı çizgi:** §4.4 · §4.5
- **Araç:** agent `tappa-db-migrator`
- **Commit:** `feat(store): add first sqlc queries`

**Amaç.** M2–M5'ün ihtiyaç duyacağı çekirdek sorguları tanımlamak ve
`internal/store`'u üretmek.

**Dokunulacak dosyalar.** `db/queries/tags.sql`, `employees.sql`,
`sessions.sql`, `transactions.sql`, `locations.sql` → `internal/store/*` (üretim)

**Asgari sorgu seti.**
| Sorgu | Not |
|---|---|
| `GetTagByUID` | status ve location dahil |
| `AdvanceTagCounter` `:one` | **atomik**; `pgx.ErrNoRows` → replay → reject |
| `GetEmployeeBySessionHash` | oturum doğrulama tek sorguda |
| `GetLastOpenTransaction` | yön tayininin temeli — son açık giriş |
| `GetLastTransactionForEmployee` | debounce (kişi bazlı, 60 sn) |
| `InsertTransaction` | tek yazım yolu |
| `GetLocationByIP` | *proof of place*, `static_ips` içinde arama |
| `ListLocationsForTenant` | GPS yedek yolu için |

**Kabul kriterleri.**
- Her sorguda **açık** `tenant_id` filtresi (RLS'e ek — kuşak + kemer). **Tek
  istisna:** `GetTagByUID` ve `GetEmployeeBySessionHash` — tenant çözümleme yolu;
  gerekçesi ve sınırları M1-01 ADR'si madde 7'de. Bu ikisi ayrı bir dosyada
  (`db/queries/resolve.sql`) durur ki istisna görünür kalsın.
- `AdvanceTagCounter` tek ifade, `WHERE last_ctr < @ctr`, `RETURNING`.
- `AdvanceTagCounter` **`ctr − last_ctr − 1` farkını da döndürür** (`tap:ctrGap`,
  [M3-06](m3-policy-motoru.md) `base:ctr-gap-review`'in girdisi).
- `make gen` temiz; üretilen `internal/store/*.go` commit edildi.
- `emit_interface: true` sayesinde `Querier` arayüzü var (handler testleri
  sahteleyebilsin).

**Tuzaklar.**
- `SELECT *` yerine sütunları açıkça yaz — şema büyüdükçe sqlc çıktısı sessizce
  değişir ve derleme hataları uzağa düşer.
- `AdvanceTagCounter`'ı `:exec` yapma; etkilenen satırı görmen gerekiyor.

---

## M1-09 — RLS izolasyonu ve değişmezlik testleri

- **Bağımlılık:** M1-06 · M1-07 · Q04
- **Kırmızı çizgi:** §4.3 · §4.5 · §8 (RLS testi zorunlu)
- **Commit:** `test(db): prove tenant isolation and transaction immutability`

**Amaç.** İzolasyonun ve değişmezliğin **kanıtı**. Kod incelemesi kanıt değildir.

**Dokunulacak dosyalar.** `internal/db/rls_test.go`

**Test vakaları.**
1. A bağlamında yazılan satır, B bağlamında **okunamaz** (0 satır).
2. B bağlamında A'nın `tenant_id`'siyle INSERT → **hata** (`WITH CHECK`).
3. Bağlam **hiç kurulmadan** sorgu → 0 satır (fail-closed), hata değil sessiz
   sızıntı değil. ⚠️ **Bu vaka, politika çıplak cast biçiminde yazılırsa GUC'a
   bir kez yazılmış bir bağlantıda geçemez** — [Q27](open-questions.md) ve
   [M1-01 madde 3](#m1-01--adr-0002-tenant-bağlamı-ve-rls-stratejisi)'teki
   ölçüme bak. Aynı `tappa_app` bağlantısında M1-09'un kendi sırasıyla ölçüldü:
   bir tenant transaction'ı → **vaka 6 tutuyor** (`app.tenant_id` = `''`, yani
   "boş") → hemen ardından bağlamsız sorgu → `ERROR: invalid input syntax for
   type uuid: ""`, **0 satır değil**. Yani **vaka 3 ile vaka 6 çıplak biçimde
   aynı anda sağlanamaz**; ancak GUC'a hiç yazılmamış bağlantıda sağlanır. Q27
   `NULLIF`'li biçimi seçerse ikisi birlikte tutar. Vakayı yazarken bağlantının
   GUC'a daha önce yazılıp yazılmadığı **açıkça** kurulmalı — aksi hâlde test,
   ölçtüğünü sandığı şeyi ölçmez.
4. `tappa_app` ile `UPDATE transactions` → yetki hatası.
5. `tappa_owner` ile `DELETE FROM transactions` → trigger hatası.
6. Transaction bittikten sonra aynı havuz bağlantısında `app.tenant_id` boş.
7. Her tablo için 1 ve 2 tekrarlanır (tablo listesi üzerinde tablo bazlı test).

**Kabul kriterleri.**
- Testler gerçek Postgres'e karşı koşuyor (Q04 kararına göre), sahte DB **yok**.
- **İzolasyon vakaları (1, 2, 3, 7) iki şartı birden sağlar — biri eksikse vaka
  RLS'i kanıtlamaz** (ölçümler: aşağıdaki kart düzeltmesi):
  1. **Rol:** `tappa_app` ile, yani `DATABASE_URL` havuzuyla koşar. `tappa_owner`
     superuser'dır ve RLS'i koşulsuz atlar.
  2. **Sorgu şekli:** izolasyon assertion'ının sorgusu **açık `tenant_id`
     filtresi taşımaz** (ham sorgu). Filtre varsa 0 satırın sebebi RLS değil
     `WHERE`'dir ve vaka, RLS kapalıyken bile geçer.
  **Mekanik denetim** (ikisi de grep'lenebilir olmalı): izolasyon vakalarının
  SQL'i test dosyasında **satır içi** yazılır — sqlc store çağrısı değil, ham
  `Query`/`Exec` — ve o SQL'de `tenant_id =` **geçmez**; vaka havuzu adlandırılmış
  `appPool` üzerinden alır, `ownerPool` yalnız vaka 5'te geçer.
- **Ayrı vaka (izolasyon kanıtı sayılmaz):** store sorgusu — yani §4.5'in
  zorunlu kıldığı açık `tenant_id` filtresini taşıyan gerçek sqlc sorgusu — de
  doğru sonucu verir. Bu vaka **sorgunun** doğruluğunu ölçer, RLS'i değil; kart
  düzeltmesindeki tabloya göre RLS kapalıyken de geçeceği için izolasyon
  kanıtı olarak **sayılamaz**. Ayrı adlandırılsın (ör. `TestStoreQueryFiltersByTenant`)
  ki sonraki okuyan onu izolasyon testi sanmasın.
- `make test` içinde koşuyor; DB yoksa anlamlı şekilde `t.Skip` ediyor (sessiz
  geçme değil, açık atlama mesajı).
- Yeni tablo eklendiğinde bu test listesine eklemeyi hatırlatan bir yorum var.

> **Kart düzeltmesi (2026-07-24, M0-03 uygulaması sırasında).** Tuzaklardaki şu
> cümle **yanlıştı** ve kaldırıldı: *"Testi `tappa_owner` ile koşarsan RLS `FORCE`
> sayesinde yine uygulanır."* M0-03'ün canlı RLS sondası bunu yanlışladı.
>
> **Ölçüm.** `FORCE ROW LEVEL SECURITY`'nin kendisi çalışıyor: geçici olarak
> yaratılan **NOSUPERUSER** bir tablo sahibi, `ENABLE`+`FORCE` RLS'li kendi
> tablosunu bağlam kurmadan okuduğunda **0 satır** gördü. Ama `tappa_owner`
> `POSTGRES_USER` olarak initdb'nin **bootstrap superuser'ıdır** (`rolsuper=t`,
> `rolbypassrls=t`, OID 10) ve **superuser RLS'i koşulsuz atlar** — `FORCE` ona
> erişemez. Yukarıdaki vakalar `tappa_owner` ile koşuldu (A, B, C tenant'larından
> birer satır):
>
> | vaka | beklenen | `tappa_app` | `tappa_owner` |
> |---|---|---|---|
> | 1 — bağlam B iken A'nın satırı okunamaz | 0 satır | 0 | **1** |
> | 2 — bağlam B iken A'nın `tenant_id`'siyle INSERT | hata | `ERROR: new row violates row-level security policy` | **`INSERT 0 1`** |
> | 3 — bağlam hiç kurulmadan sorgu | 0 satır | 0 | **3** |
>
> **Yanlış rolle koşmak sessiz değil, gürültülüdür.** Yukarıdaki üç vaka
> `tappa_owner` altında **patlar** — tehlike "yanlış güven" değil, bozukluğun
> yanlış yerde (RLS'te) aranmasıdır; oysa roldedir.
>
> **Asıl tehlike role değil, sorgunun şekline bağlıdır.** CLAUDE.md §4.5
> sorgularda RLS'e **ek olarak** açık `tenant_id` filtresi ister ("kuşak+kemer"),
> yani M1'in gerçek store sorguları o filtreyi taşıyacak. Vaka 1 aynı iddiayla
> ("A'nın satırı — `id=1` — okunamaz, 0 beklenir") iki sorgu şeklinde ölçüldü:
>
> | sorgu şekli | `tappa_app` | `tappa_owner` | vaka ne ölçüyor |
> |---|---|---|---|
> | ham: `ctx=B`, `WHERE id=1` | **0** ✅ | **1** ❌ | RLS gerçekten sınanıyor |
> | §4.5 store biçimi: `ctx=B`, `WHERE id=1 AND tenant_id=<B>` | **0** ✅ | **0** ✅ | **iki rolde de geçer** — RLS hiç sınanmıyor |
>
> İkinci satırda 0'ın sebebi `WHERE`'dir: satır 1 zaten A'nındır, filtre onu
> eleyecektir — RLS kapalı olsa da vaka geçer. Yani **rol kriterini tek başına
> koymak yetmez**: vaka 1 ve 7, `tappa_app` + `DATABASE_URL` şartına tam uyarak,
> sqlc store sorgularıyla yazılıp RLS hakkında hiçbir şey kanıtlamayabilir. Bu,
> CLAUDE.md §8'in zorunlu kıldığı testin sessizce yeşile boyanmasıdır.
>
> Kabul kriteri bu yüzden **iki boyutlu** yazıldı — rol **ve** sorgu şekli.
> İkisi çelişmez, farklı şeylerdir: **üretim** sorguları §4.5 gereği filtreyi
> taşımak **zorundadır**; **izolasyon testi** ise RLS'in tek başına yaptığı işi
> ölçmek zorundadır, dolayısıyla filtreyi taşımamalıdır. Filtreli biçim ayrı bir
> vaka olarak durabilir ama izolasyon kanıtı sayılmaz.
>
> Tuzağın **doğru** kalan kısmı korundu: vaka 4 (`tappa_app` ile `UPDATE`) ile
> vaka 5 (`tappa_owner` ile `DELETE`) farklı roller ister, yani iki havuz yine
> gerekir — ama gerekçesi "`FORCE` sayesinde owner da RLS'e tabi" **değil**.

**Tuzaklar.**
- **İki ayrı havuz gerekir, ama roller karıştırılmamalı.** Vakaların tam rol
  haritası:

  | vaka | rol | neden |
  |---|---|---|
  | 1, 2, 3, 7 — izolasyon | `tappa_app` | RLS yalnız bu rolde yürürlükte |
  | 4 — `UPDATE transactions` yetki hatası | `tappa_app` | yetki reddi bu rolde ölçülür |
  | 5 — `DELETE FROM transactions` trigger hatası | `tappa_owner` | trigger'ın superuser'ı da durdurduğunu kanıtlar (§4.3) |
  | 6 — tx sonrası `app.tenant_id` boş | `tappa_app` | havuz davranışı; uygulama rolüyle anlamlı |

  Hangi vakanın hangi rolle koştuğu testte açıkça görünsün.
- Paralel testlerde tenant UUID'lerini sabitleme; her test kendi tenant'ını yaratsın.

---

## M1-10 — Seed verisi ve sabit ID'ler

- **Bağımlılık:** M1-06
- **Araç:** skill `tappa-seed`
- **Commit:** `feat(fixtures): add KF and KM seed data`

**Amaç.** İki design partner'ın gerçek yapısını veritabanına koymak: KF (9
lokasyon) ve KM (5 departman), çalışanlar, plaketler, vardiyalar.

**Neden.** Dashboard, entegrasyon testleri ve satış demoları aynı veriyi
kullanacak. Tutarsız demo verisi, hatalı motoru doğru gösterir.

**Dokunulacak dosyalar.** `test/fixtures/seed.sql`, `test/fixtures/ids.go`

**Kabul kriterleri.**
- `make seed` idempotent (sabit UUID + `ON CONFLICT DO NOTHING`), iki kez
  koşulunca ikinci kez de temiz geçiyor.
- Skill'deki tablo birebir: 9 KF lokasyonu doğru vardiyalarla, Rusty Bar
  `overnight = true`, 5 KM departmanı kendi vardiyalarıyla.
- IP'ler dokümantasyon aralığından (`203.0.113.0/24`, `198.51.100.0/24`);
  gerçek müşteri IP'si repoda **yok**.
- AES anahtarları **sahte** ve öyle etiketlenmiş.
- İki tenant arasında paylaşılan tek satır yok (RLS testinin zemini).
- `test/fixtures/ids.go` sabitleri var; testlerde sihirli UUID string'i yok.
- GPS koordinatları Malta'da ve birbirinden ≥ birkaç yüz metre (150 m testi
  anlamlı olsun).

**Tuzaklar.**
- Zamanlar `now()`'a **göreli** üretilir; sabit takvim tarihi gömme, yoksa
  dashboard bir hafta sonra boş görünür.
- Seed **yalnızca** master veri koyar. İşlem (`transactions`) üretimi M5-09'daki
  "bir günü simüle et" işine ait — orada karar motorunun **çıktısıyla** üretilir,
  elle uydurulmaz.
- Malta yerel saatinden UTC'ye çeviriyi seed içinde açıkça yap ve yorumla.

---

## M1-11 — Migration 0006: admin kullanıcıları

- **Bağımlılık:** M1-02 · Q03
- **Kırmızı çizgi:** §4.5 · §4.7
- **Araç:** agent `tappa-db-migrator`
- **Commit:** `feat(db): add admin users and admin sessions`

**Amaç.** Panele giren kişinin şemasını kurmak.

**Neden.** Denetimde çıktı: M6-01 "şifre hash'i Q03'e göre", M7-04 "sıfırlama
token'ının hash'i saklanıyor" diyor ama **bu hash'lerin yazılacağı tablo hiçbir
migration görevinde yok**. `employees`'da şifre alanı yok ve olmamalı — çalışan
hiç şifre görmez, ürünün vaadi bu. Tablo M1'de doğmazsa M6-01 gizlice bir
migration + RLS + test + seed işi taşır.

**Alanlar.**
```
admin_users(id, tenant_id, full_name, email citext, password_hash, role,
            status, created_at, last_login_at)     -- role: owner|manager
admin_sessions(id, tenant_id, admin_user_id, token_hash, created_at,
               last_used_at, revoked_at)
password_resets(id, tenant_id, admin_user_id, token_hash, expires_at, used_at)
```

**Kabul kriterleri.**
- Üç tabloda da RLS beşlisi tam.
- `password_hash` Q03 kararına göre; düz veya hızlı hash **yok**.
- `admin_sessions` çalışan `sessions` tablosundan **ayrı** — bir çalışan çerezi
  panele, bir admin çerezi tap akışına geçemez (M6-01 bunu şart koşuyor).
- `password_resets` tek kullanımlık: `used_at` damgalanır, satır silinmez.
- `role` alanı var; yetkilendirme kararları policy motorundan gelir
  (`actor:role` bağlam anahtarı, `sys:policy-edit-owner-only` guardrail'i).
- M1-09 RLS test listesine üç tablo da eklendi.
- Seed (M1-10) her tenant için bir `owner` üretiyor.

**Tuzaklar.**
- `admin_users`'ı `employees`'a bir bayrakla eklemek cazip; yapma. İki farklı
  kimlik türü, iki farklı oturum ömrü, iki farklı tehdit modeli.
