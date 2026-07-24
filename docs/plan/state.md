# Durum

> **Bu dosya projenin tek canlı durum kaynağıdır.** Her oturumun sonunda
> güncellenir ([README.md](README.md) → oturum protokolü, adım 6.4).
> Görev kartlarına durum işareti konmaz.

**Son güncelleme:** 2026-07-24 (2. oturum)

---

## ŞU AN

| | |
|---|---|
| **Kilometre taşı** | M0 — [Bootstrap](m0-bootstrap.md) |
| **Sıradaki görev** | **M0-07** (`make check` + `make audit` yeşile alma; artık serbest) → sonra **M0-06** (CI) → M0 kapanır |
| **Çalışma modu** | Orkestrasyon + üçüncü göz — [README.md](README.md) · brief'ler [agent-brief.md](agent-brief.md) |
| **Dal** | `m0-bootstrap` — ilk commit `7e12f37` `main`'de; M0'ın kalanı dalda, M0 bitince `main`'e birleşir (CLAUDE.md §10) |
| **Blokeler** | **Yok.** Bekleyen dört kullanıcı girdisi de geldi (Docker açıldı · Go 1.26.5 · Q04 · Q01) ve Q25 + Q27 karara bağlandı. |

**Bir sonraki oturum ne yapmalı:** M0-07'yi yaptır (kapsamı bu oturumda büyüdü —
aşağıdaki nota bak), sonra M0-06, sonra M0'ı `main`'e birleştir ve M1'e geç.
M1 artık bekleyen karar olmadan yazılabilir.

**M0-07'nin kapsamı büyüdü.** Kartın kendi Bulgu 1'i (staticcheck SA1019 →
`middleware.RealIP` router'dan çıkarılır) **artı** bu oturumda karara bağlanan
üç araç düzeltmesi (Q25 a/b/d) **artı** arm64 Go geçişi. Kartın **Bulgu 2'si
düştü** — Go 1.26.5 ile `govulncheck` temiz.

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
| M0-06 | CI iş akışı | todo | Q04 = yerel Postgres → CI'da `services: postgres:17` · M0-07'ye bağlı |
| M0-07 | make check ve make audit'i yeşile alma | todo | **serbest.** Bulgu 1 (SA1019) + Q25 a/b/d + arm64 geçişi. Bulgu 2 düştü (Go 1.26.5 → govulncheck temiz) |

### M1 — [Veri katmanı](m1-veri-katmani.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M1-01 | ADR 0002: tenant bağlamı ve RLS stratejisi | todo | Q27 kararı normatif yazılır · M0-03 ölçümleri buraya taşınabilir |
| M1-02 | Migration 0001: tenants | todo | Q01 ✔ (`timezone`) · Q27 ✔ |
| M1-03 | Migration 0002: locations & departments | todo | Q01 ✔ (`locations.timezone` override) · **Q07 açık** · Q25 (c) burada |
| M1-04 | Migration 0003: employees & sessions | todo | |
| M1-05 | Migration 0004: tags | todo | |
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

**Özet:** 82 görev · done 5 · wip 0 · blocked 0 · skipped 1 · todo 76

---

## Oturum günlüğü

En üste ekle. Kısa tut: ne yapıldı, ne öğrenildi, ne kaldı.

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
