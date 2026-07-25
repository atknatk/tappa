# Durum

> **Bu dosya projenin tek canlı durum kaynağıdır.** Her oturumun sonunda
> güncellenir ([README.md](README.md) → oturum protokolü, adım 6.4).
> Görev kartlarına durum işareti konmaz.

**Son güncelleme:** 2026-07-25 (3. oturum)

---

## ŞU AN

| | |
|---|---|
| **Kilometre taşı** | **M1 — [Veri katmanı](m1-veri-katmani.md)** (M1-01…M1-05 done) |
| **Sıradaki görev** | **M1-06** — [Migration 0005: transactions (append-only) & audit_log & transaction_reviews](m1-veri-katmani.md#m1-06--migration-0005-transactions-append-only--audit_log). Bekleyen karar yok. **En kritik immutability görevi** (§4.3 UPDATE/DELETE yok, §4.6 kayıt kaybolmaz). DELETE tuzağı (yukarı) burada **zorunlu**: `REVOKE UPDATE, DELETE` + trigger. Bağımlılık M1-04+M1-05 ✔. |
| **Çalışma modu** | Orkestrasyon + üçüncü göz — [README.md](README.md) · brief'ler [agent-brief.md](agent-brief.md) |
| **Dal** | **`main`** — M0 (`m0-bootstrap`) `main`'e fast-forward birleştirildi (`562f021`), dal silindi. **Kullanıcı kararı (2026-07-25): artık doğrudan `main`'de çalışılır, görev başına dal açılmaz** (CLAUDE.md §10 güncellendi). Push/PR yine istemedikçe yok. |
| **Blokeler** | Yok (M1-04 için bekleyen karar yok). **Bekleyen kullanıcı eylemi:** arm64 Go kurulumu (aşağı bak) — hiçbir şeyi bloklamıyor. |

**Bir sonraki oturum ne yapmalı:** **M1-06** (transactions append-only + audit_log +
transaction_reviews). **En kritik immutability görevi.** Kritik noktalar (kart):
- **§4.3 mekanik immutability:** `REVOKE UPDATE, DELETE ON transactions FROM tappa_app`
  (GRANT yalnız SELECT+INSERT) **+ ayrıca** `BEFORE UPDATE OR DELETE` trigger `RAISE
  EXCEPTION` (tappa_owner yolunu da kapatır — kuşak+kemer). audit_log ve
  transaction_reviews de append-only aynı kalıpla. **DELETE tuzağı** (aşağı) burada zorunlu.
- **§4.6 kayıt kaybolmaz:** `employee_id`, `location_id`, `department_id` **NULLABLE**
  olmak zorunda — §5 satır 1-2 (etiket lost / SUN geçersiz) satır 3'ten (oturum yok) önce
  gelir; çerezsiz biri çalınmış plakete dokununca `employee_id`'siz `reject` kaydı yazılmalı.
  Hangi verdict'te hangi alanların null olabileceği CHECK ile sabitlenir.
- **`(tag_uid, ctr)` üzerine UNIQUE KOYMA** (reddedilen replay de aynı çifti taşır → kayıt kaybı).
- `transaction_reviews` **üç kısıtla** doğar: `UNIQUE(transaction_id)`; `transaction_id`
  yalnız `verdict='flag'` kayda; `reviewer_id <> transactions.employee_id` (kendini onaylama
  yasak, `sys:no-self-review`). Append-only + RLS beşlisi tam.
- İndeksler: `(tenant_id, occurred_at DESC)`, `(tenant_id, employee_id, occurred_at DESC)`.
  `source_ip inet`, `trust smallint`, `occurred_at` (§5 `sys:occurred-at-bound` guardrail'i
  ileride) ≠ `created_at`. Bağımlılık M1-04 (employees) + M1-05 (tags) ✔.

**M1-08/M1-10'a devredilen (M1-05 denetiminden):** `aes_key_ref`'in gerçekten KEK-sarmalı
olduğu şema düzeyinde zorlanamaz (bytea) → insert-yolu (M1-08) ve seed (M1-10) bunu ayrıca
doğrulamalı; KEK'in DB dışında (config `TAPPA_TAG_KEK`) tutulması çözümleme güvenliğinin ön
şartı (M8 deploy denetimi).

**M1-03'ten devralınan iki not (bloklamayan, yapıcının eklediği ekstra kısıtlar):**
- **M4-05:** `locations.shift_*` nullable → geç kalma hesabı null vardiyayı "hesaplanmaz"
  ile ele almalı. Ayrıca `shift_pair` CHECK **tek-yönlü vardiyayı** (yalnız shift_start)
  reddediyor — ileride esnek-saat lokasyonu gerekirse bu kısıt gözden geçirilir.
- **M1-10:** seed tüm lokasyonlarda **çift-uçlu** vardiya kullanmalı (shift_pair CHECK).
  Ufak tutarsızlık: `overnight=true` + NULL vardiya migration'da kabul ediliyor (zararsız,
  domain yok sayar); seed overnight'ı yalnız dolu vardiyayla kullansın.

**Migration numaralandırma:** goose `-s` (sequential), 5 haneli — `00001_...`,
`00002_...`. Makefile `migrate-new` artık `-s` geçiyor (M1-02'de düzeltildi).

**⚠️ Planlı kırmızı durum (M1-02→M1-07):** `make gen`/`make dev`/`make build` sqlc
adımında **"no queries contained in paths"** ile patlar — sorgular M1-08'e ait
(M1-08 ledger notu: "ilk sorgu bunları yeşile çevirir"). **`make check` bundan
etkilenmez** (fmt+lint+test+temiz-diff; sqlc çalıştırmaz) ve CI yeşil kalır. Migration
doğrulaması sqlc'ye değil goose+psql'e dayanır. Bu regresyon değil, plan sonucu.

**M1'e girmeden önce hazır olması gerekenler** (hepsi çözüldü):
Q01 (timezone) ✔ · Q04 (yerel Postgres) ✔ · Q27 (`NULLIF`) ✔.

### M1-05 için devralınan gereksinim (ADR 0002 madde 7 — M1-04'te kurulan kalıp)

Çözümleme yolu **çevrelenmiş (bounded) bypass** olarak M1-04'te sessions için
kuruldu ve iki denetçi (üçüncü göz + tappa-security-auditor) tarafından ampirik
onaylandı. **M1-05 tags ayağını AYNI kalıpla kurar:**
- `tappa_resolver` rolü **zaten var** (db-init: NOLOGIN, BYPASSRLS, NOSUPERUSER,
  default privilege YOK). M1-05 ona **`tags`'te sütun-düzeyi SELECT** verir (yalnız
  çözümleme için gerekenler) + `resolve_tag_by_uid(...)` SECURITY DEFINER fonksiyonu:
  **owner tappa_resolver** (superuser DEĞİL), **`SET search_path=pg_catalog, pg_temp`**,
  gövde `public.tags` nitelenmiş, **`REVOKE ALL ... FROM PUBLIC` + yalnız tappa_app'e
  EXECUTE**, `uid` PK → ≤1 satır. Fonksiyon ihtiyaçtan fazla sütun döndürmesin.
- tags RLS politikası **standart NULLIF** — resolve OR-dalı **YOK** (GUC-anahtar saf-RLS
  alternatifi denetimde reddedildi: SET LOCAL'siz GUC havuzda kalıp toplamsal OR ile
  çapraz-tenant sızdırıyor + `FOR ALL USING` WITH CHECK'i kopyalıyor; ADR 0002 madde 7
  ve "Değerlendirilen alternatifler"e kaydedildi).
- Güvenlik RLS'ten değil **arayüzden**: beş kısıt (anahtar girdi · ≤1 satır · SELECT*
  yüzeyi yok · yalnız EXECUTE · naif "NULL iken satır" dalı yasak). Sınırı M1-09'da test.

### DELETE tuzağı — M1-05, M1-06 ve immutability isteyen her tablo (M1-04'te bulundu)

`ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner ... GRANT ... DELETE ... TO tappa_app`
(db-init) **her yeni tabloda** tappa_app'e DELETE'i otomatik verir. Bir tablodan silmeyi
engellemek için GRANT'tan DELETE'i çıkarmak **YETMEZ** (GRANT yalnız ekler) — açık
**`REVOKE DELETE ON <tablo> FROM tappa_app;`** gerekir (M1-04 sessions/employees böyle
yaptı). **M1-06 `transactions`/`audit_log` için bu ZORUNLU** (§4.3 immutable: `REVOKE
UPDATE, DELETE` + trigger). Ampirik doğrulandı: REVOKE'suz DELETE başarıyla koşuyordu.

### ⏳ Bekleyen kullanıcı eylemi — arm64 Go kurulumu

Q26 kararı: yerel toolchain arm64'e geçecek. **Repo işini bloklamıyor** — her şey
amd64 Go 1.26.5 ile yeşil; kazanç yalnız yerel derleme/test hızı (Rosetta ~2-3x
yavaş). Orkestratör go.dev tarball'ını indirdi ve **checksum'ı go.dev ile birebir
doğruladı** (`efb87ff2…`), ama `/usr/local`'a kurulum **sudo parolası** ister —
kullanıcı çalıştırmalı. Komutlar oturum notunda. Yapılınca `go version` →
`darwin/arm64` olur ve ilk `make gen` bir kez uzun sürer (build cache + pinli CLI
önbellekleri tazelenir — bozukluk değil).

**Not:** M0-05 (ilk commit) sıradan **öne alındı** — kullanıcı "arada commit at"
dedi. Bundan sonra her onaylanan görevin ardından bir commit atılır.

**Politika ve kapsam kararları: hepsi karara bağlandı** — Q14…Q27 cevaplandı,
gerekçe ve etkilenen kartlar [open-questions.md](open-questions.md) →
Cevaplananlar'da.

**Kalan açık sorular (Q02, Q03, Q05–Q13)** teknik/ticari; hiçbiri M0'ı veya M1'in
başını bloklamıyor. En yakın blokajlar: Q07 (`static_ips` tipi) → M1-03,
Q03 (admin şifre hash'i) → M1-11/M6-01, Q05+Q06 (SDM modu, anahtar stratejisi) → M2-01.

### M1-09 için devralınan bulgular (M0-03 3. tur denetiminden)

Denetçi kabul kriterini yenen **üç kaçış yolu** buldu. Bloklayan sayılmadılar
(kriter bugünkü hâliyle de M0-03'ün gerektirdiğinden fazlasını yapıyor), ama
M1-09 brief'ine **girmeleri zorunlu** — yoksa yeşil ve anlamsız bir test seti çıkar:

1. **grep sağlam değil.** `tenant_id =` taraması `'<B>'::uuid = tenant_id`,
   `tenant_id IN ('<B>')`, `tenant_id::text = '<B>'` biçimlerini kaçırıyor; üçü de
   RLS **kapalıyken de** 0 satır veriyor. Bağlayıcı olan düzyazı şart, grep işaret.
2. **Pozitif kontrol istenmiyor.** Vaka 3 boş tabloda kritere tam uyar ve hiçbir şey
   kanıtlamaz. Her izolasyon vakası, aynı ham sorgunun **doğru bağlamda >0** döndüğünü
   de göstermeli; korumayı kapatınca test **kırmızıya dönmeli**.
3. **Rol boyutu çalışma anında kanıtlanmıyor.** Kriter `appPool`/`ownerPool`
   **adlandırmasına** bakıyor; owner kimlikli havuzu `appPool` diye adlandıran test
   geçer. Doğrusu: testin içinde `SELECT current_user` = `tappa_app` **ve**
   `rolsuper/rolbypassrls = f,f` assertion'ı (ikisi de `tappa_app` ile çalışıyor).

Ayrıca kapsam dışı iki tutarsızlık: `scripts/db-init/01-roles.sql:3` ve
[m0-bootstrap.md:59](m0-bootstrap.md) bypass'ı "tablo sahibi + BYPASSRLS" diye
anlatıyor, **superuser'dan söz etmiyor** ve `FORCE`'un salt sahipliği yendiğini
yazmıyor (ölçüldü). Güvenlik sonucu yok, temkinli yönde yanlış — M1-01 ADR 0002
yazılırken düzeltilmeli.

**Kabul edilen riskler** (ADR 0005, [M3-09](m3-policy-motoru.md)): buddy
punching · sahte GPS · **URL biriktirme** · mekânda proxy · müdürün kimlik
basması. Hiçbiri çözülmedi; hepsinin tespit sinyali [M6-11](m6-dashboard.md)'de.

---

## Sağlık kontrolü

İşe başlamadan çalıştır. Beklenen çıktıyı vermeyen bir satır varsa **önce onu
düzelt**.

| Komut | Beklenen |
|---|---|
| `go version` | `go1.26.2` veya üstü |
| `go build ./...` | çıktı yok (temiz) |
| `git log --oneline \| head -3` | M0-05 sonrası en az 1 commit |
| `git status --short` | M0-05 sonrası temiz |
| `ls .env` | M0-01 sonrası var (git'e **girmez**) |
| `docker compose ps` | M0-03 sonrası `tappa-db` ayakta |
| `make migrate-status` | M1 boyunca uygulanan migration listesi |
| `make check` | M0-06 sonrası yeşil |

---

## Ledger

Durumlar: `todo` · `wip` · `done` · `blocked` · `skipped`
Bir görev `done` olurken commit hash'i yazılır. `blocked`/`skipped` ise **neden**
yazılır.

### M0 — [Bootstrap](m0-bootstrap.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M0-01 | .env ve kriptografik anahtarlar | **done** | commit yok (`.env` ignore'da) · üçüncü göz 2. turda ONAY · kart düzeltildi (F2) |
| M0-02 | Go bağımlılıkları (pgx, uuid, templ) | **done** | `e6d9a63` · üçüncü göz 3. turda ONAY · kart iki kez düzeltildi |
| M0-03 | Postgres ve rol ayrımı doğrulaması | **done** | üçüncü göz **3. turda** ONAY · kart düzeltildi · iki ölçüm M1'i bağladı (→ Q27, ve M1-01/M1-02/M1-09 kartları güncellendi) |
| M0-04 | Üretim hattı doğrulaması (templ · sqlc · tailwind) | **done** | `2521d48` · üçüncü göz 2. turda ONAY · `sqlc.yaml`'da 3 bozuk override bulundu ve düzeltildi |
| M0-05 | İlk commit ve dal stratejisi | **done** | `7e12f37` · sıradan öne alındı (kullanıcı isteği) · orkestratör yaptı, M0-02 denetiminde doğrulanacak |
| M0-06 | CI iş akışı | **done** | üçüncü göz **1. turda** ONAY · `make up`+`make check`+`make audit`, Go 1.26.5 pinli, ripgrep kurulu, Node yok · iki kart sapması ölçümle doğrulandı (`CGO_ENABLED=1`, `services:` yerine `make up`) |
| M0-07 | make check ve make audit'i yeşile alma | **done** | üçüncü göz **2. turda** ONAY · SA1019 (RealIP çıkarıldı) + Q25 a/b/d · redline R5 üç sessiz atlatma turunda yeniden yazıldı (lexer) · **arm64 hâlâ açık** (aşağı bak) |

### M1 — [Veri katmanı](m1-veri-katmani.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M1-01 | ADR 0002: tenant bağlamı ve RLS stratejisi | **done** | `4eb3780` · üçüncü göz **2. turda** ONAY (1. tur RED: madde 7 superuser SECURITY DEFINER çelişkisi) · Q27 (`NULLIF`) + M0-03 superuser/FORCE ölçümleri normatif · iki tutarsızlık (01-roles.sql, m0-bootstrap.md) + kart madde 7 örneği düzeltildi |
| M1-02 | Migration 0001: tenants | **done** | `aff4ced` · üçüncü göz **1. turda** ONAY · RLS beşlisi (id-PK istisnası) canlı doğrulandı, policy birebir `NULLIF`, fail-closed/WITH CHECK/pozitif kontrol tappa_app ile geçti, Down çalışıyor, R5 mutasyonla kanıtlandı · Makefile `migrate-new` `-s` düzeltmesi · kart adım 3 NULLIF'e güncellendi |
| M1-03 | Migration 0002: locations & departments | **done** | `3d66b17` · üçüncü göz **1. turda** ONAY · RLS beşlisi (iki tablo) + çapraz-tenant bileşik FK + `cidr[]` (Q07) + `numeric(9,6)` + Down + R5 mutasyonla kanıtlandı · 2 bloklamayan kısıt notu (→ M4-05/M1-10) · Q25(c) sqlc override M1-08'e ertelendi |
| M1-04 | Migration 0003: employees & sessions | **done** | `2c42c67` (+ db-init resolver rölü) · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi ihlali yok) · ADR 0002 madde 7 çözümleme mekanizması: `tappa_resolver` (BYPASSRLS) + `resolve_session_by_token_hash` SECURITY DEFINER (owner non-superuser, search_path sabit, PUBLIC REVOKE, kolon-SELECT) — enumerate/search_path/PUBLIC/injection saldırılarına dayandı · **GUC-anahtar alternatifi denetimde reddedildi** (ADR'ye kaydedildi) · sessions/employees DELETE `REVOKE` edildi (default-privilege tuzağı) · ADR 0002 + M1-04 kartı güncellendi |
| M1-05 | Migration 0004: tags | **done** | `a1bcdc4` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi ihlali yok) · `resolve_tag_by_uid` çözümleme fonksiyonu (M1-04 kalıbı; enumerate/pg_temp-poison/PUBLIC/injection saldırılarına dayandı) · uid `char(14)` hex CHECK · `aes_key_ref bytea` sarmalı · **`UNIQUE(uid,ctr)` YOK** (§4.4) · DELETE REVOKE · replaced_by same-tenant self-FK · aes_key_ref-sarmalı doğrulaması M1-08/M1-10'a devredildi |
| M1-06 | Migration 0005: transactions (append-only) & audit_log | todo | |
| M1-07 | internal/db: havuz ve tenant kapsamlı transaction | todo | Q27 telafisi: sarmalayıcı `SET LOCAL`'i kendi kurar, bağlamsız sorgu **API olarak imkânsız** |
| M1-08 | İlk sqlc sorguları | todo | ilk sorgu `make gen`/`make dev`/`make build`'i yeşile çevirir |
| M1-09 | RLS izolasyonu ve değişmezlik testleri | todo | Q04 ✔ (yerel Postgres) · **ŞU AN bölümündeki "devralınan bulgular" brief'e girmeli** |
| M1-10 | Seed verisi ve sabit ID'ler | todo | |
| M1-11 | Migration 0006: admin kullanıcıları | todo | Q03 · denetim bulgusu |

### M2 — [SUN doğrulama](m2-sun.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M2-01 | ADR 0003: SDM modu ve anahtar yönetimi | todo | Q05, Q06 |
| M2-02 | AES-CMAC (RFC 4493) | todo | |
| M2-03 | SDM URL ayrıştırma | todo | |
| M2-04 | Oturum anahtarı, kısaltılmış MAC, sabit zamanlı karşılaştırma | todo | |
| M2-05 | Anahtar sarmalama (KEK) | todo | |
| M2-06 | Atomik sayaç ilerletme ve eşzamanlılık testi | todo | **§4.4 — en kritik** |
| M2-07 | sun.Verify ve test vektörleri | todo | kapsam %90+ |

### M3 — [Policy motoru](m3-policy-motoru.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M3-01 | ADR 0004: policy motoru modeli | todo | |
| M3-02 | Policy şeması (append-only sürümler) | todo | |
| M3-03 | Belge modeli, ayrıştırma ve doğrulama | todo | |
| M3-04 | Değerlendirici (koşullar, öncelik, açıklanabilirlik) | todo | |
| M3-05 | Guardrail politikaları | todo | **§4 — en kritik** |
| M3-06 | Tappa Baseline yönetilen politikası | todo | A2, A3, Y1 varsayılanları |
| M3-07 | Kararın kayda bağlanması | todo | |
| M3-08 | Test seti ve gevşetilemezlik kanıtı | todo | kapsam %90+ |
| M3-09 | ADR 0005: kabul edilen riskler | todo | Q19 — buddy punching, sahte GPS |

### M4 — [Tap karar motoru](m4-tap-motoru.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M4-01 | internal/geo: haversine ve yarıçap | todo | |
| M4-02 | Karar girdi/çıktı tipleri | todo | |
| M4-03 | Decide(): bağlam kurma ve kararın uygulanması | todo | M3-04 üstünde |
| M4-04 | Yön tayini (in/out) | todo | |
| M4-05 | Vardiya çözümü ve geç kalma | todo | Q01 |
| M4-06 | Trust puanı, QR kanalı, practice tap | todo | |
| M4-07 | Tablo bazlı test seti | todo | kapsam %90+ |

### M5 — [Tap akışı](m5-tap-akisi.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M5-01 | internal/session: oturum yaşam döngüsü | todo | Q11 |
| M5-02 | Davet ve aktivasyon akışı | todo | Q02 |
| M5-03 | Middleware: gerçek IP, tenant, oran sınırı | todo | |
| M5-04 | GET /t: tap sayfası | todo | skill tappa-brand |
| M5-05 | POST /api/checkin: orkestrasyon | todo | **§4.3/4.4/4.6** |
| M5-06 | Onay ekranı ve marka mesajları | todo | |
| M5-07 | Mini tur ve practice tap | todo | |
| M5-08 | QR kanalı | todo | |
| M5-09 | Uçtan uca test ve "bir günü simüle et" | todo | |
| M5-10 | Tap tazelik penceresi (URL biriktirmeye karşı) | todo | A1 · §4.4 |

### M6 — [Admin dashboard](m6-dashboard.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M6-01 | Admin kimlik doğrulama | todo | Q03 |
| M6-02 | Dashboard iskeleti ve docket bileşenleri | todo | |
| M6-03 | Transactions sekmesi | todo | |
| M6-04 | FLAGGED onay kuyruğu | todo | **§4.3** |
| M6-05 | Employees sekmesi | todo | |
| M6-06 | Locations & Wall Tags sekmesi | todo | |
| M6-07 | Reports ve CSV export | todo | |
| M6-08 | Manuel kayıt girişi | todo | |
| M6-09 | Policy yönetim ekranı | todo | guardrail kilitli gösterilir |
| M6-10 | Policy simülatörü | skipped | Q22 → M9-06'ya ertelendi |
| M6-11 | Anomali ve kötüye kullanım raporu | todo | A1·A3·A4·Y-D·Y-E sinyalleri |
| M6-12 | Çalışan sayımı ve fatura taslağı | todo | Q24 |

### M7 — [Portal & signup](m7-portal.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M7-01 | Landing sayfası | todo | |
| M7-02 | Kayıt sihirbazı ve VAT | todo | Q09 |
| M7-03 | Tenant provisioning | todo | |
| M7-04 | Admin daveti, şifre sıfırlama, e-posta | todo | Q02 |
| M7-05 | Hesap ve marka mesajı ayarları | todo | |

### M8 — [Deploy & pilot](m8-deploy-pilot.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M8-01 | Paketleme | todo | |
| M8-02 | Barındırma | todo | Q08, Q12 |
| M8-03 | Gözlemlenebilirlik | todo | |
| M8-04 | Güvenlik denetimi | todo | |
| M8-05 | Plaket encode runbook | todo | Q06, Q10 |
| M8-06 | KF St Julians pilotu | todo | Q13 · yasal pilot kapısı |
| M8-07 | Üretim tenant kurulumu ve cihaz envanteri | todo | denetim bulgusu |

### M9 — [Pilot sonrası](m9-sonrasi.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M9-01 | Çevrimdışı kuyruk | todo | MVP dışı |
| M9-02 | Yönetici push bildirimleri | todo | MVP dışı |
| M9-03 | BioTime CSV içe aktarma | todo | MVP dışı |
| M9-04 | Tenant marka mesajı editörü | todo | MVP dışı |
| M9-05 | Çalışan self-service saat görünümü | todo | MVP dışı |
| M9-06 | Policy simülatörü | todo | Q22 — M6-10'dan ertelendi |
| M9-07 | Ham JSON politika editörü | todo | Q22 — M6-09'dan ayrıldı |

**Özet:** 82 görev · done 12 · wip 0 · blocked 0 · skipped 1 · todo 69 · **M0 tamam · M1: M1-01…M1-05 done**

---

## Oturum günlüğü

En üste ekle. Kısa tut: ne yapıldı, ne öğrenildi, ne kaldı.

### 2026-07-25 (3. oturum, devam) — **M1-05 done** (tags)

**M1-05 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi
görevi). `00004_create_tags.sql` (`a1bcdc4`): tags + RLS beşlisi (standart NULLIF) +
`resolve_tag_by_uid` çözümleme fonksiyonu (M1-04 kalıbı — owner tappa_resolver superuser
değil, search_path sabit, PUBLIC REVOKE, kolon-SELECT, ≤1 satır uid PK). uid `char(14)`
hex CHECK, `aes_key_ref bytea` sarmalı, `last_ctr` yalnız durum, **`UNIQUE(uid,ctr)` YOK**
(§4.4 — reddedilen replay de kaydedilebilmeli), DELETE açık REVOKE.

**Adversarial denetim (tags):** enumerate, **pg_temp poisoning** (sahte TEMP tags →
fonksiyon gerçek public.tags döndürdü), `public.tags_evil` yaratma (denied), SET ROLE
(denied), uid injection — hepsi başarısız. **aes_key_ref maruziyeti kabul edilebilir +
mimari zorunlu** (SUN/CMAC tenant bağlamından önce anahtarı ister; sarmalı ref KEK olmadan
atıl, uid public, EXECUTE yalnız tappa_app).

**İki gerekçeli sapma (denetçi sound buldu):** replaced_by same-tenant composite self-FK
(+ UNIQUE(uid,tenant_id); çapraz-tenant pointer'ı yapısal engeller) · replaced_by redundant
hex CHECK (zararsız). İkisi de güvenliği artırıyor.

**İleriye devredildi (M1-08/M1-10):** aes_key_ref-sarmalı doğrulaması şema düzeyinde
zorlanamaz → insert-yolu + seed ayrıca doğrulamalı (yukarı "ŞU AN").

**Sırada:** M1-06 (transactions immutable + audit_log + transaction_reviews) — en kritik
immutability görevi.

### 2026-07-25 (3. oturum, devam) — **M1-04 done** (employees, sessions, tenant çözümleme)

**M1-04 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi
görevi olduğu için kuşak+kemer). `00003_create_employees_sessions.sql`: employees +
sessions, RLS beşlisi (standart NULLIF), biyometri yok, `token_hash` UNIQUE (token asla),
composite same-tenant FK. Commit `2c42c67`.

**En kritik parça — tenant çözümleme mekanizması (ADR 0002 madde 7).** "Kimlik doğrulama
yolundaki tek delik". Kararı GUC-anahtar mı yoksa ADR'nin SECURITY DEFINER'ı mı diye
**kullanıcıya sordum → "önce denetle"**. Bir tasarım denetçisi GUC-anahtar saf-RLS
alternatifini canlı Postgres'te **iki tek-nokta hatasıyla kırdı** (SET LOCAL'siz resolve
GUC → toplamsal OR çapraz-tenant READ sızıntısı, NULLIF yakalamıyor; `FOR ALL USING`
WITH CHECK'i kopyalıyor → WRITE forge). Çevreleme **yapısal değil, disipline bağlıydı** →
**reddedildi**. ADR'nin kararı doğruymuş; gerekçesi ("saf RLS ifade edemez") yanlıştı →
madde 7 düzeltildi + GUC-anahtar reddedilen alternatif olarak kaydedildi.

**Kurulan yapısal mekanizma:** `tappa_resolver` rolü (db-init: NOLOGIN, BYPASSRLS,
NOSUPERUSER, default privilege YOK) + `resolve_session_by_token_hash` SECURITY DEFINER
(owner tappa_resolver **superuser değil**, `search_path=pg_catalog,pg_temp` sabit + gövde
`public.sessions` nitelenmiş, `REVOKE ALL FROM PUBLIC` + yalnız tappa_app EXECUTE, kolon-
düzeyi SELECT, ≤1 satır UNIQUE). Denetçiler **enumerate · search_path injection (gerçek
pg_temp.sessions kurdu) · PUBLIC EXECUTE · SET ROLE · param injection** saldırılarını
denedi, hepsi başarısız. tappa_app fonksiyon olmadan çapraz-tenant session okuyamıyor.

**db-init rölü + re-init:** kullanıcı "db-init'e ekle + re-init" seçti. `docker compose
down -v` reddedildi + Docker daemon internet kesintisinde düşmüştü → daemon'ı yeniden
başlattım, rolü tappa_owner ile **elle** oluşturdum (db-init'in taze konteynerde yapacağının
aynısı — volume silmeden, dev DB ⇄ CI tutarlı).

**DELETE tuzağı (ikinci denetçi + yapıcı buldu):** GRANT'tan DELETE çıkarmak yetmiyor —
`ALTER DEFAULT PRIVILEGES` her tabloya DELETE veriyor; açık `REVOKE DELETE` gerekti
(sessions/employees). **M1-06 transactions immutability için ZORUNLU** — yukarı "DELETE
tuzağı" bloğuna işlendi + agent-brief dersine eklendi.

**Sırada:** M1-05 (tags) — çözümleme mekanizmasının tags ayağı, M1-04 kalıbının aynısı.

### 2026-07-25 (3. oturum, devam) — **M1-03 done** (locations & departments)

**M1-03 done — üçüncü göz 1. turda ONAY.** `00002_create_locations_departments.sql`:
iki tablo, her ikisinde RLS beşlisi (`NULLIF` policy, USING+WITH CHECK). Çapraz-tenant
FK: `locations UNIQUE(id, tenant_id)` + `departments` bileşik FK `(location_id,
tenant_id)→locations` ON DELETE RESTRICT (+ doğrudan tenant FK). `static_ips cidr[]
NOT NULL DEFAULT '{}'` (Q07), `gps numeric(9,6)` (float değil), `shift_* time` +
`overnight bool`, `locations.shift_*` ve `departments.shift_*` nullable. Denetçi canlı
doğruladı: fail-closed/pozitif/izolasyon/WITH CHECK (iki tablo, tappa_app), çapraz-tenant
FK reddi (owner), cidr[] içerme, Down, R5 **mutasyonla** (GRANT+FORCE sildi→ayrı flag).

**Yapıcı savunmacı ekstra kısıtlar ekledi** (işaretledi, kapsam içi/aynı tablo): gps/shift
pair + gps aralık CHECK'leri, `(tenant_id, location_id)` bileşik indeks. Denetçi ikisini
sorguladı (bloklamıyor): shift_pair tek-yönlü vardiyayı reddediyor · overnight=true+null
vardiya kabul (tutarsız ama zararsız). İkisi de master veri, §4.6-güvenli → M4-05/M1-10'a
devredildi (yukarı "ŞU AN"da).

**Q25(c) ertelendi:** sqlc.yaml cidr[] override'ı M1-08'e — sqlc sorgu olmadan koşamaz,
doğrulanamaz (M0-04 dersi). pgx/v5 zaten cidr'i netip.Prefix'e eşliyor; M1-08'de
GetLocationByIP ile birlikte eklenip doğrulanır.

**Sırada:** M1-04 (employees & sessions) — ADR 0002 madde 7 çözümleme mekanizması
(sessions) brief'e girmeli.

### 2026-07-25 (3. oturum, devam) — **M1-02 done** (tenants migration)

**M1-02 done — üçüncü göz 1. turda ONAY.** `db/migrations/00001_create_tenants.sql`:
`tenants` tablosu + RLS beşlisi (`tenants` istisnası: scope anahtarı `id`, `tenant_id`
değil — ADR 0002 madde 5). Policy birebir `id = NULLIF(current_setting('app.tenant_id',
true), '')::uuid` (USING+WITH CHECK). Denetçi canlı doğruladı: fail-closed (bağlamsız→0,
doğru bağlam→1) + WITH CHECK (yanlış-id INSERT hatası) + pozitif kontrol, hepsi
**tappa_app** (rolsuper=f, rolbypassrls=f) ile; Down gerçekten çalışıyor (DROP TABLE
policy+grant'ı düşürüyor); redline R5 **mutasyonla** kanıtlandı (4 sabotaj yolu da
yakalandı). Yapıcı `tappa-db-migrator` idi, kapsam **yalnız migration** (sqlc/test
M1-08/M1-09'a bırakıldı — sınır korundu).

**Kararlar:** `vat_number NOT NULL UNIQUE` (global tekil, format app'te), `plan
CHECK(founding|standard) DEFAULT founding` (M6-12 founding uyarısını okuyacak),
`structure/business_type CHECK` (enum değil — goose Down temiz), `timezone` Q01.

**İki keşif kapatıldı:** (1) Makefile `migrate-new` `-s` geçmiyordu → timestamp isim
üretiyordu; `-s` eklendi, artık `00001/00002...` sequential. (2) `make gen`/`dev`/`build`
sqlc'de "no queries" ile patlıyor — **planlı** (M1-08 ilk sorguyla yeşile döner);
`make check` sqlc çalıştırmadığı için etkilenmiyor, CI yeşil.

**settings.json:** oturumda beş izin (docker compose down · git push · git commit ·
gh pr create · go get) ask→allow taşınmış; kullanıcı **olduğu gibi bırak** dedi,
şeffaf chore commit'iyle kaydedildi (§10 davranışı değişmez: istemedikçe push/PR yok).

**Sırada:** M1-03 (locations & departments) — **Q07 kararı gerekir** (static_ips tipi),
başlamadan sorulacak.

### 2026-07-25 (3. oturum) — M0 `main`'e birleşti, **M1-01 done**

**İki kullanıcı kararı.** (1) `m0-bootstrap` → `main` fast-forward birleştirildi
(`562f021`), dal silindi. (2) **Bundan sonra doğrudan `main`'de çalışılır, görev
başına dal açılmaz** (kullanıcı: "yeni proje, sürekli branch gereksiz"). CLAUDE.md
§10 buna göre güncellendi (`88b775e`); push/PR yine istemedikçe yok.

**M1-01 done — üçüncü göz 2. turda ONAY.** ADR 0002 (tenant bağlamı + RLS)
yazıldı: rol ayrımı, tx-başına `set_config('app.tenant_id',$1,true)`, normatif
politika ifadesi `NULLIF(current_setting('app.tenant_id', true), '')::uuid`
(Q27), kuşak+kemer açık filtre, `tenants` öz-koruması, MVP'de süper-admin yok, ve
**tenant çözümleme istisnası**. M0-03 ölçümleri (tappa_owner superuser + FORCE,
izolasyon testi tappa_app/DATABASE_URL, ham sorgu vs §4.5 filtresi) normatif not.

**1. tur RED — gerçek kusur.** Madde 7, çözümleme mekanizması olarak superuser
`tappa_owner`'a ait `SECURITY DEFINER` fonksiyon öneriyordu — ama superuser gövdesi
RLS'i tümüyle atlar, yani ADR'nin kendi "genel bypass açılmaz" şartını ihlal eden
**genel bir bypass**. Ben (orkestratör) briefte bu gerilimi denetçiye sordurdum;
denetçi bağımsız buldu. **Öğrenilen teknik gerçek:** saf RLS sorgunun **şekline**
göre kısıtlama ifade edemez (satır bazlı boolean, `WHERE`'i göremez) → çözümleme
kaçınılmaz olarak sınırlı bir bypass ister; iş onu **çevrelemektir** (arayüz beş
kısıtı; definer superuser olamaz; §6 FORCE altında **yalnız BYPASSRLS**).

**2. tur ONAY + iki bloklamayan gözlem kapatıldı.** (a) ADR'ye "§6 FORCE altında
salt-SELECT yetersiz, bypass yalnız BYPASSRLS olabilir" sınır netliği eklendi
(M1-04/05 tuzağını kapatır). (b) Kart madde 7'nin ADR'nin çürüttüğü "sütun bazında
kısıtlı politika" örneği düzeltildi + görünür kart düzeltme bloğu ("yanlışlanan
kartı da düzelt" dersi). Küçük doküman düzeltmeleri orkestratörce doğrulandı.

**M1-04/M1-05'e devredilen gereksinim** yukarıda "ŞU AN"da yazılı (çevrelenmiş
bypass yüzeyi, BYPASSRLS sınırı) — brief'e girmesi zorunlu.

**Sırada:** M1-02 (Migration 0001: tenants) — bekleyen karar yok.
**⏳ Kullanıcıya:** arm64 Go kurulumu hâlâ açık (iki komut, sudo), bloklamıyor.

### 2026-07-24 (2. oturum, devam) — M0-06 kapandı, **M0 TAMAMLANDI**

**M0-06 done — üçüncü göz 1. turda ONAY** (bu oturumun ilk tek-turluk onayı).
`.github/workflows/ci.yml`: `push`+`pull_request`, tek job, `actions/checkout@v4`
+ `actions/setup-go@v5` (Go **1.26.5** pinli), ripgrep kurulur, `make tools` →
`make up` → `make check` → `make audit`. **Node yok**, üçüncü parti action yok,
action'lar pinli.

**İki kart sapması ölçümle doğrulandı:** (1) `CGO_ENABLED` kartta `0` yazıyordu →
**`1`** olmalı: `make check` `go test -race` koşuyor ve linux/amd64'te race detector
cgo ister (`GOOS=linux CGO_ENABLED=0 go test -race` → `-race requires cgo`, **sıfır
test dosyasıyla bile**). (2) Postgres `services: postgres:17` bloğuyla **değil**,
`make up` (compose) ile: `services:` konteynerleri checkout'tan **önce** başlar,
repo'nun `db-init/01-roles.sql`'ini uygulayamaz → `tappa_app` rolü hiç oluşmaz.

**Q04 metni düzeltildi:** "CI'da `services: postgres:17`" cümlesi infeasible'dı ve
sevk edilen CI ile çelişiyordu; uzlaştırma notu eklendi (kararın özü değişmedi,
yalnız CI'da nasıl ayağa kalktığı). Denetçinin "yanlışlanan kartı da düzelt" bulgusu.

**M0'ın yedi görevi:** M0-01 (2 tur) · M0-02 (3 tur) · M0-03 (3 tur) · M0-04 (2 tur) ·
M0-05 (ilk commit) · M0-06 (1 tur) · M0-07 (2 tur). Biri (M6-10) proje genelinde
`skipped`. **M0 milestone tamam.**

**Sırada:** `m0-bootstrap` → `main` birleştirme (**kullanıcı kararı**, sor) → M1-01.

**⏳ Kullanıcıya hatırlatma:** arm64 Go kurulumu hâlâ açık (iki komut, sudo).

### 2026-07-24 (2. oturum, devam) — M0-07 kapandı, `redline-check.sh` yeniden yazıldı

**M0-07 done — üçüncü göz 2. turda ONAY.** Dört iş: (1) `middleware.RealIP`
router'dan çıkarıldı (SA1019; §5'te 50 güven puanı taşıyan IP'nin altına
sahtelenebilir değer koymamak) · (2) `make seed` yerel `psql` yerine
`docker compose exec` (yeni `scripts/seed.sh`) · (3) `govulncheck` **v1.6.0**'a
pinlendi · (4) `redline-check.sh` R5 dosya düzeyinden **tablo düzeyine** taşındı.
`make check` ve `make audit` **yeşil**; Bulgu 2 (stdlib CVE) Go 1.26.5 ile düşmüştü.

**1. tur RED — tarayıcının kendisi yalancıydı.** R5'te üç sessiz atlatma vardı:
kapsam-sütunu kontrolü `tenants` dışında **hiç tetiklenemiyordu** (aranan
`tenant_id`, politikanın zorunlu yazdığı `app.tenant_id` GUC adının içinde geçiyor)
· `/* */` blok yorumu beş kontrolü de susturuyordu · `-- +goose Down` bölümü Up'ın
şartlarını karşılıyordu. Yapıcının 13 vakalık sondası bunları kaçırdı çünkü
gerçekte yazılacak biçimi hiç denemedi — `agent-brief.md`'ye yeni ders olarak
işlendi ("sonda ürünün gerçek girdisiyle yapılır").

**2. tur ONAY.** `sed`+`tr` atıldı, yerine durum makineli **SQL lexer** (`sql_lex`)
+ goose Up kesici yazıldı. Denetçi lexer'a 11 kaçış yoluyla saldırdı (iç içe yorum,
E-string, dolar-etiketli gövde, `DO $$`, sonlandırılmamış tırnak…) ve **yapısal
değişmezi** doğruladı: maskeleme metni silmiyor → Up'taki her `CREATE TABLE` en
kötü ihtimalle görünür WARN üretir, asla sessiz-yeşil geçemez.

**İki konvansiyon sıkılaştı:** `tenants` istisnası artık niteliksiz/`public.` +
PK'nın `id` üzerinde olmasını arıyor (`archive.tenants` kaçışı kapandı); muafiyet
yorumu yalnız Up `^--` satırından okunuyor ve **her koşuda WARN** basılıyor
(sessiz muafiyet kapandı).

**M1'e devredilen redline notları (bloklamayan):** `E'\''` E-string lexer durumunu
bozup sonraki ifadeyi WARN'a düşürüyor (sessiz değil) — M1 migration'larında
E-string kullanılmamalı, "R5 denetleyemedi" WARN'ı elle doğrulanmalı · iç içe blok
yorumu desteklenmiyor (yalnız yanlış-pozitif yönü) · muafiyet `$$` gövdesi içinde
de okunabiliyor ama WARN'lanıyor · tek dosyada O(tablo²) performans (goose'un
küçük-migration konvansiyonuyla sorun değil).

**Kapsam dışı gözlem:** `tappa_owner` `rolsuper=t` (M0 init'ten geliyor); M0-03'te
de görülmüştü, M1-01 ADR 0002 yazılırken gözden geçirilmeli.

**Sırada:** M0-06 (CI) → M0 kapanır → `main`'e birleştir → M1.

**⏳ Kullanıcıya:** arm64 Go kurulumu iki komut, sudo parolası ister — orkestratör
tarball'ı indirip checksum'ını doğruladı, kalanı kullanıcının.

### 2026-07-24 (2. oturum) — M0-03 kapandı, altı karar alındı, blokeler bitti

**Ortam:** Docker açıldı, Go **1.26.5**'e yükseldi → `govulncheck` **temiz**.
M0-07'nin Bulgu 2'si (dört stdlib CVE) kendiliğinden düştü. Toolchain hâlâ
Rosetta (`darwin/amd64`); arm64 geçişi M0-07'ye alındı.

**M0-03 done — üçüncü göz üç tur sürdü, üçünde de gerçek kusur çıktı.**
Kabul kriterleri **ilk turda** karşılanmıştı (`tappa_app` NOBYPASSRLS/NOSUPERUSER,
iki rol ayrı ve ikisiyle de bağlanılıyor, `pgcrypto`+`citext` çalışıyor). RED'lerin
üçü de yapıcının **kart dışına çıkıp** yaptığı canlı RLS sondasının ürettiği
bulgulardan çıktı — sonda meşruydu ve değerliydi, kartın üç kriteri RLS'in *ön
şartını* ölçüyor, RLS'in kendisini değil.

1. **1. tur RED:** ölçüm doğru, çıkarım ters. "`tappa_owner` ile koşan izolasyon
   testi her zaman *sızıntı yok* der" **yanlış** — M1-09'un üç vakasında da
   gürültülü patlıyor. Ayrıca bulgunun yanlışladığı `m1-veri-katmani.md` satırına
   hiç dokunulmamıştı → repoda iki çelişik cümle.
2. **2. tur RED:** düzeltme olarak eklenen kriter yalnız **rolü** bağlıyordu.
   Oysa tehlike **sorgunun şekli**: `ctx=B, WHERE id=1 AND tenant_id=B` biçimi
   iki rolde de 0 satır verir — kritere tam uyumlu bir test RLS'i hiç sınamaz.
3. **3. tur ONAY.** Kriter iki boyutlu oldu (rol **ve** ham sorgu şekli), §4.5 ↔
   izolasyon testi ayrımı yazıldı, düşen "test edilir" garantisi geri kondu,
   filtreli biçim **ayrı** ve *izolasyon kanıtı sayılmayan* bir vaka oldu.

**M1'i bağlayan iki ölçüm:**
- `app.tenant_id` GUC'una bir kez **yazılınca** bağlantıda `NULL`'a dönmüyor, `''`
  kalıyor (`ROLLBACK`/`RESET`/`DISCARD ALL` üçü de). Tetikleyici **yazma**, kullanım
  sayısı değil. → **Q27**.
- `FORCE ROW LEVEL SECURITY` tablo **sahibini** bağlar, **superuser'ı bağlamaz**;
  `tappa_owner` initdb'nin bootstrap superuser'ı olduğu için kaçıyor. NOSUPERUSER
  bir sahiple ölçülerek doğrulandı (`ENABLE`-only → 3 satır, `+FORCE` → 0).

**Altı karar:** Q01 (`tenants.timezone` + `locations.timezone` override) ·
Q04 (DB testleri yerel Postgres) · Q26 (toolchain yükseltildi, arm64'e geçilecek) ·
Q25 a/b/d (seed `docker compose exec`, govulncheck pinlenir, redline R5 genişler) ·
**Q27** (`NULLIF` sarmalayıcısı — CLAUDE.md §6 güncellendi). Açık soru 14 → **11**.

**CLAUDE.md §6'ya iki madde eklendi:** politikaların `NULLIF`'li biçimi ve
"izolasyon testi ile üretim sorgusu farklı şekiller ister" ayrımı. İkincisi
olmadan §4.5'in kuşak+kemer kuralı, RLS testini sessizce anlamsızlaştırıyordu.

**M1-09'a devredilen üç kaçış yolu** yukarıda "ŞU AN" bölümünde yazılı — brief'e
girmeleri zorunlu.

**Sırada:** M0-07 (kapsamı büyüdü) → M0-06 → `main`'e birleştir → M1.

### 2026-07-24 — dış denetim (3 ajan) ve bulguların işlenmesi

Kod yazılmadı. Plan üç bağımsız ajana okutuldu: **tutarlılık**, **güvenlik**,
**pratiklik**. Bulguların tamamı [open-questions.md](open-questions.md) →
"İkinci denetim" tablosunda, nereye işlendikleriyle birlikte.

**En önemli sonuç: A1 (URL biriktirme) çözülmemişti.** M5-10 tazelik penceresi
`GET /t` anından başlıyor; saldırgan uçak modunda 10 kez dokunup URL'leri
toplayabiliyor — sunucu o okumaları hiç görmüyor. Önceki oturumda "✅ çözüldü"
işaretlemem yanlıştı, düzeltildi. A1 artık **kabul edilen risk** (ADR 0005) +
`tap:ctrGap` sinyali (Q21).

Diğer üç yapısal bulgu: `occurred_at` istemciden geliyor ve guardrail'siz (K1) ·
motor yetkilendirmede fail-open (K2) · tenant çözümlemesi RLS bağlamından önce
gelmek zorunda (K3). Üçü de karşılandı: **altı yeni guardrail** eklendi
(`sys:tenant-mismatch`, `sys:occurred-at-bound`, `sys:policy-edit-owner-only`,
`sys:no-self-review` + `ignore`/`redirect` kilidi + guardrail **sırası** normatif).

Dört karar (Q21–Q24): A1 politikaya · M3 v1 daraltıldı (simülatör ve JSON
editörü M9'a) · yasal metinler dağıtıldı + **pilot kapısı** · tahsilat elle,
sayım otomatik.

Yeni görevler: M1-11 (admin şeması — hiç yoktu), M6-12 (fatura taslağı),
M8-07 (üretim tenant + telefon envanteri), M9-06/07 (ertelenenler).
M6-10 `skipped`. Görev sayısı 76 → **81**.

Sırada: M0-01. Açık soru sayısı 13 → **14** (Q25 küçük araç düzeltmeleri).

### 2026-07-24 — policy motoru plana eklendi, milestone'lar kaydırıldı

Kod yazılmadı. Plan gözden geçirildi; kötüye kullanım analizinde dört ciddi açık
(URL biriktirme, QR + sahte GPS, GPS sahteciliği, buddy punching) ve yedi
mantık boşluğu bulundu — özeti [open-questions.md](open-questions.md) A/Y
maddelerinde.

Çözüm olarak **policy motoru** (AWS IAM benzeri belge yapısı, üç katman:
guardrail / baseline / tenant) yeni **M3** milestone'u olarak eklendi; eski
M3–M8 birer basamak kaydı (M4–M9). Hiç commit ve tamamlanmış görev olmadığı için
yeniden numaralandırma bedelsizdi. Görev sayısı 63 → **75**:
M3 (8 yeni) · M5-10 tazelik penceresi · M6-09/10/11 policy ekranı, simülatör,
anomali raporu.

Tap kararları artık kod içi `if` zinciri değil: §5 satır 1–5 **guardrail**
(kapatılamaz), satır 6–7 **baseline** (tenant değiştirebilir). `tap.Decide`
bağlam kurar ve effect'i uygular.

Aynı oturumda **yedi karar** alındı ve işlendi (Q14–Q20):
WiFi adımı · QR'da IP zorunlu · GPS-only tenant anahtarı · çapraz lokasyonda
tap edilen lokasyonun vardiyası · **unutulan çıkışta otomatik kapatma YOK**
(açık kayıtlar saat toplamına girmez, rapor eksikliği açıkça söyler) ·
buddy punching kabul edilen risk (ADR 0005 → yeni **M3-09**) · onaylar ayrı
`transaction_reviews` tablosunda.

Etkilenen kartlar güncellendi: M1-06, M3-06, M3-09 (yeni), M4-04, M4-05, M5-02,
M5-08, M6-04, M6-07, M8-06. CLAUDE.md §5'e geç kalma, unutulan çıkış ve QR
maddeleri işlendi. Görev sayısı 75 → **76**.

Sırada: M0-01.

### 2026-07-24 — planlama altyapısı kuruldu

Kod yazılmadı. `docs/plan/` oluşturuldu: roadmap, 63 görev kartı (o günkü
numaralandırmayla M0–M8; policy motoru eklenince kaydı),
bu durum dosyası, 13 açık soru. Yol haritası sırası handoff §10'dan bilinçli
olarak farklı — gerekçe [roadmap.md](roadmap.md#neden-dashboard-1-değil-6-sırada).

Repo durumu: iskelet dosyalar var ve derleniyor (`go build ./...` temiz), ama
`db/migrations`, `db/queries`, `web/templates` boş; `internal/` altında yalnız
`config` ve `httpx` var. Commit geçmişi yok, `.env` yok, Docker kapalı.

Sırada: M0-01.

### 2026-07-24 — M0 yürütmeye başlandı: orkestrasyon + üçüncü göz

Çalışma modu değişti ([README.md](README.md) → Çalışma modu): ana oturum iş
yapmaz, her görevi bir Opus alt ajana yaptırır ve **ayrı** bir üçüncü göz ajanı
onaylayana kadar düzelttirir.

Dört görev kapandı: **M0-01** (2 tur), **M0-02** (3 tur), **M0-04** (2 tur),
**M0-05** (ilk commit, sıradan öne alındı). Commit'ler: `7e12f37`, `e6d9a63`,
`2521d48`. Dal `m0-bootstrap`.

**Dördü de ilk turda RED aldı ve her seferinde gerçek bir kusur çıktı** —
hayali bulgu yok. En değerlisi M0-04'teki üç bozuk `sqlc.yaml` override'ı
(nullable `uuid` → geçersiz Go · `inet` → var olmayan paket, üstelik sqlc
exit 0 veriyordu · nullable `timestamptz` override'ı hiç yoktu). Üçü de
iskeleden beri oradaydı ve M1'de kesin patlardı.

Kart hataları da bu turlarda çıktı ve düzeltildi: M0-01'in `go run` kriteri
ulaşılamazdı (`.env`'i Makefile yüklüyor) · M0-02'nin `go mod tidy` adımı kendi
önceki adımlarını siliyordu · M0-04'ün sqlc kriteri fazla gevşekti.

Yeni görev **M0-07** (`make check` + `make audit` yeşile alma) ve yeni soru
**Q26** (Go ≥1.26.5, arm64) denetimden doğdu.

**M0'ın kalan üçü de kullanıcı girdisi bekliyor** — burada duruldu.

Bağlam sıkıştırması öncesi [agent-brief.md](agent-brief.md) yazıldı: yapıcı ve
denetçi brief şablonları, her turda tekrarlanan sabit kurallar ve M0'da
öğrenilen dokuz ders. Bunlar o ana kadar yalnız sohbette taşınıyordu; artık
repoda.

**Kullanıcıdan beklenen dört girdi:** Docker Desktop (M0-03) · Q26 Go ≥1.26.5
arm64 (M0-07) · Q04 DB testi hedefi (M0-06) · Q01 zaman dilimi (M1-02).
