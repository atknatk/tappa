# M15 — İş günü ve yön zinciri: gün dönümü, tahmini yön + düzeltme, eksik yarı talebi

> **Durum:** plan (2026-10-21, 6. sürüm — 5. sürüm: motor merceği şartlı ONAY, güvenlik ve ürün dar RED;
> bulgular ve kullanıcı kararları K5–K6 işlendi). Önceki turlar: 4. sürüm (üç mercek RED + K1–K4), 5. sürüm
> (üç mercek RED, KRİTİK yok). Kod yok. Canlı durum yalnız [state.md](state.md)'dedir;
> bu dosyaya "yapıldı" yazılmaz.
>
> **Karar belgesi:** [ADR 0032](../adr/0032-is-gunu-ve-yon-zinciri.md) (durum *Önerildi*; 0031
> M14'ündür). Kartlar ADR'nin sözlüğünü kullanır; ADR kabul edilmeden (M15-01) kod kartı başlamaz.
>
> **Atıf kuralı:** kırmızı çizgiler hep **"CLAUDE.md §4.x"**; `file:line` atıfları HEAD `0e7c158`'e
> göredir ve yapıcı kendi turunda yeniden okur.
>
> **Test adı kuralı:** `TestEveryNamedTestExists` reponun bütün metin dosyalarında geçen her `Test…`
> adının var olmasını ister (alt çizgiyle biten önekler, o önekle başlayan bir test varsa geçer). Bu
> belge yalnız **var olan** testleri ve `TestDecide_` gibi var olan önekleri adlandırır; yeni testler
> **tarif edilir**, adlarını yapıcı koyar. Bir kart bir testi yeniden adlandırırsa ona atıf yapan
> belgeyi (bu dosya ve ADR 0032 dahil) **aynı commit'te** günceller.
>
> **Sevk:** **Faz A, M15-08 kapanınca B ve C'yi beklemeden ayrıca canlıya çıkar (K6, 2026-10-21); B + C
> toplu geçişle.** Gerekçe: ters zincir canlıda her gün yeni, değişmez, yanlış kayıt üretiyor; A bunu
> durdurur. Faz A önce çıktığı için yayın anahtarı (`TAPPA_BUSINESS_DAY_FROM`, M15-04) Faz B'den önce zaten
> etkin olur — Faz B onsuz sevk edilmez (M15-11, M15-14). Faz A'nın sevk kapısı M15-02…05,
> M15-07 ve M15-08'dir; **M15-06 kapının dışında** — 5. sürümün (e) kuralıyla motorun çıkışı Faz A'da hiç
> (b)/(c) olmaz (ADR 0032 §A.6), yani M15-05'in geçici eşlemesi üretimde ulaşılmazdır. M15-06 Faz B'nin
> `want` alanından (M15-11) önce biter. `state.md` aynı cümleyi söyler.

## Sorun — ölçülmüş kök neden

Kullanıcı (2026-10-21): *"okutmalarda in/out olmadığı için saçma çalışıyor, girerken out
yapabiliyor."*

`internal/domain/tap/decide.go:533-544` `resolveDirection`: kişinin son **açık** girişi varsa bu
dokunuş `out`'tur — **pencere yok**. `StaleOpenIn` (18 sa, `decide.go:481`) yalnız not düşer, yön yine
`out`. Sorgu `GetLastOpenTransaction` da tarihsizdir. Sonuç: **bir kez** unutulan çıkış zinciri **kalıcı
olarak** ters çevirir:

| Gün | Gerçek | Kaydedilen | Neden |
|---|---|---|---|
| Pzt 09:12 | giriş | `in` | açık giriş yok |
| Pzt 17:30 | çıkış (dokunmadı) | — | unutuldu |
| Sal 09:05 | giriş | **`out`** + stale notu | Pzt girişi hâlâ açık |
| Sal 17:40 | çıkış | **`in`** | açık giriş yok |
| Çar 09:00 | giriş | **`out`** | Sal 17:40 `in`'i açık |

Rapor (`accumulate`) kaydedilmiş yönü sırayla eşlediği için ters zincir **gece aralıklarını** sayar
(Sal 17:40 → Çar 09:00 = 15 sa 20 dk < 18 sa `closingTail` → ödenir).

## Kullanıcı kararları (2026-10-21) — bağlayıcı

Yeniden tartışılmaz; ayrıntı ve gerekçe ADR 0032'de.

- **Faz A — iş günü / gün dönümü saati.** Açık giriş yalnız **aynı iş günündeyse ve `StaleOpenIn`'den
  yeniyse** kapatılır; değilse bu dokunuş `in`'dir, eski giriş *"çıkış eksik"* anomalisi kalır. Yön
  politika değil **hesaptır** (§5). `tap.Decide` saf kalır. Ayar ekranı vardiya çakışmasını kaydetmeden
  önce uyarır. Geçmiş ters kayıtlar düzelmez (CLAUDE.md §4.3).
  - **K1 — kademe: işletme → şube → departman → kişi**, en özel kazanır, varsayılan 05:00. Şube =
    çalışanın **profil** şubesi (`employees.location_id`), dokunduğu şube değil; zincir kişinindir. KF'de
    departman yok — orta kademeyi şube doldurur.
- **Faz B — tap ekranında tahmini yön + düzeltme** (CLAUDE.md §9 onayı). *"Clock in"* / *"Clock out"* +
  *"Not right? Clock out"*. Verdict değişmez, kayda not, `transactions.direction_source`.
- **Faz C — eksik yarı talebi + müdür onayı** (§9 istisnası). Talep kayıt değildir: `punch_claims`;
  onay → `channel='manual'` kayıt + `audit_log`.
  - **K2 — tek okutma:** geçmiş gün kartı yalnız *"What time did you leave?"* sorar. Tek okutmanın aslında
    çıkış olduğu gün anomali ekranında kalır — kabul edilmiş sınır (ADR 0032 §C.1).
  - **K3 — saat alanı:** vardiya saatiyle önceden dolu; müdüre giriş saati, oluşacak süre ve
    *"varsayılan değiştirilmedi"* işareti; vardiya ± §A.7 payı dışındaki talep tek tıkla onaylanamaz;
    vardiyasız çalışanda alan boş ve zorunlu.
  - **K4 — en çok iki kez:** yeni iş gününün ilk dokunuşunda, atlanırsa aynı iş gününün çıkış
    dokunuşunda bir kez daha; sonra yalnız anomali ekranı.

## Brief iddialarının doğrulaması (2026-10-21, `0e7c158`)

| Brief iddiası | Kodda | Sonuç |
|---|---|---|
| `resolveDirection` penceresiz toggle; `StaleOpenIn` yalnız not | `decide.go:533-544`, `:481` | ✅ doğru |
| Kök sorgu `GetLastOpenTransaction` | `transactions.sql:16-84` | ✅; sorgu yalnız bir `@at` üst sınırı ve `id` beraberlik bozucusu alır (M15-02, ADR 0032 §A.4) |
| `anomalies.sql` `StaleOpenIn`'i kullanıyor | `anomalies.sql:650` | ❌ **yalnız bir yorumda**. Gerçek okuyucular: `ledger/report.go:168`, `:905`, `ledger/floor.go:122`, `ledger/tape.go:110, :178`, `ledger/site.go:164` |
| Ledger de aynı kurala uymalı mı? | `ledger/report.go:964-988` | ⚠️ **Evet — anomali listesi ve talep kapısıyla birlikte, tek bir sınıflandırmayla** (ADR 0032 §A.6). Kart M15-05, M15-06 |
| `tap.Decide` saf kalır, saat dilimi/DST çağıranda | `decide.go:423-446`, `tzdata.go` | ⚠️ **kısmen yanlış öncül:** Decide bugün de saat dilimi çözüyor (geç kalma). `Input`'a kesim + bölge girer (ADR 0032 §A.3) |
| Hangi saat dilimi | `00001:34`; `locations`'ta sütun yok | Yalnız `tenants.timezone`; Q01'in `locations.timezone`'u **hiç uygulanmadı** |
| Tap ekranı metin dili | `tap.templ:117-123`, `view.go:389-399` | İngilizce: düğme **"Tap"**, sonuç *"Tapped in / out"* (Q33) |
| `occurred_at`, 72 sa, M9-01 | `guardrails.go:79`, `baseline.go:229` | ✅; `base:queued-window` 120 sn; M9-01 **todo** ama `occurred_at` alanı canlı |
| Sonuç ekranında buton yok | `result.templ:8-13` | ✅ |
| `transaction_reviews` | `00005:260-356` | Yalnız `flag` satırına karar — talepler giremez (M15-15) |
| ADR 0011 çatışıyor mu | ADR 0011, `manual.go:85-147` | Çatışmıyor; S dolu varyantlar için **(e) fazla çıkış** sınıfı gerekir (M15-05, ADR 0032 §A.6) |
| Advisory kilit (ADR 0006) | `LockEmployeeForTap` | ✅ |
| ADR 0031 boş · son migration | `00063_*` | ❌ **0031 M14'ün** (`origin/docs/m14-identity-plan`, `573731d`) → bu ADR 0032; son migration `00063` ✅ |
| Profil şubesi (K1) | `00003:32`, `00037:88` | `employees.location_id` — `00037` `DROP NOT NULL` yaptı (M11'in API ile yönetilen çalışanı, K-14); NULL'da şube kademesi atlanır |

**Okurken ve mercek turlarında bulunanlar** (ayrıntı ADR 0032 *"Bulunan riskler"*):

1. **Tek çıkış sınıflandırması** — üç okuyucu (liste, rapor, talep kapısı) aynı tanımı kullanır; kapatmayan
   bir çıkış **altı ayrık sınıftan** tam birindedir (ADR 0032 §A.6): kapatır · (a) çok uzun → `TooLong` ·
   (b) başka iş günü ve (c) yetim → `OutWithoutIn` · (e) fazla çıkış ve (d) önsüz-S-NULL →
   `StartedEarlier`. **(e)** yönetici düzeltmesinin (*"out too late"*) bıraktığı ikinci çıkıştır (motor
   merceği M-Y1): çıkışın iş gününde bir giriş var (`I`) **ve** önceki çıkış ondan **sonra yazılmış**
   (`W`: `P.created_at > out.created_at`). 4. sürümde yalnız `I` vardı ve moladan sonra unutulan girişi
   yutuyordu (iki merceğin ortak YÜKSEK bulgusu, 5. sürümde `W` ile kapandı).
2. **KM'nin vardiyaları 05:00 ile çakışıyor** (`test/fixtures/seed.sql:195-211`). Q30.
3. **Form ve `tap.js`** — `form.submit()` submitter'ı göndermez; konum API'siz yolda devre dışı submitter
   değer göndermez (`tap.js:67-71`, `:97-103`); statik gizli alan JS'siz telefonda iki `want` üretir.
4. **Talep onayıyla saat şişirme** (güvenlik G-Y1) — vardiya bandı dışında tek tık yok, müdür aralığı ve
   süreyi görür (M15-18).
5. **API akışı** (G-O6) — Q31 Faz B'nin sevk kapısı (M15-13).
6. **Saate bağlı DB testleri** (motor M-Y2) — 05:00 varsayılanı göreli saatli testleri günün belirli
   saatlerinde kırmızıya çevirir (M15-04).
7. **Kartlar kendi commit'inde yeşil olmalı** (motor M-O2) — `@at` ile çağıranı ve motorla kırılan DB alt
   testi aynı karta.
8. **Öz-onay yasağı bugün boştur** (G-O1) — vaat edilmez (ADR 0032 §C.4).
9. **Çok sıkı CHECK §4.6 kaybıdır** ve **`Down` kanıt siler** — ikinci emniyet CHECK'leri `NOT VALID` +
   ayrı doğrulama; `Down`'lar dolu satırda reddeder.

## Çalışma modu (CLAUDE.md §10) — her kart için

- **Yapıcı + ayrı üçüncü göz.** Her kartı bir yapıcı alt ajan (opus) uygular; işi yapmayan **ayrı**
  bir denetçi kabul kriterlerini tek tek doğrular; bulgu → düzeltme → **yeni** denetçi. **"Mercek:
  iki"** = ayrıca agent `tappa-security-auditor`.
- **Her kart kendi commit'inde yeşildir** — bir kartın kırdığı test o kartta düzeltilir (M-O2).
- **Testler:** tur içinde yalnız değişen testler `-run` ile + saniyelik kapılar (`gofmt`, build,
  `bash scripts/redline-check.sh`). Tam `-race`, `go test ./...`, `make check` bir kartta **bir kez**,
  commit'ten önceki son doğrulamada; fazın büyük testleri faz kapanışında (M15-08, M15-14, M15-19).
- **`make gen`** her `.templ` / `db/queries/*.sql` değişikliğinden sonra; **`make css`** her `input.css`
  değişikliğinden sonra.
- **Kapsam:** `internal/domain/tap` %90+ (CLAUDE.md §8).
- **Dal:** feature dalı. `main`'e birleştirme bir **deploy kararıdır** — kullanıcı istemedikçe yok.

## Her kartın miras aldığı kısıtlar

- **Yön hesaptır, politika değildir** (CLAUDE.md §5); `policy_context`'e yön, iş günü ya da istenen yön
  **girmez**.
- **`tap.Decide` saf.** Kademe SQL'de çözülür; motor çözülmüş kesimi ve bölgeyi alır.
- **Tek temsil.** İş günü sınırı (`S(at)`, `Next(S) = S(S + 26 sa)`) ve çıkış sınıflandırması `tap`
  paketinde tek fonksiyondur; SQL karşılığı tek fonksiyondur, parite testiyle bağlıdır.
- **Zaman UTC** (CLAUDE.md §6); kesim bir duvar saatidir — `time.Date(…, loc)`; var olmayan/iki kez
  yaşanan saat açık kodla.
- **`transactions` yalnız INSERT**; yeni sütunlar tarihsel satırlarda NULL; `Down`'lar kanıt silmez;
  `transactions` ALTER'ları `SET LOCAL lock_timeout = '1s'` ile başlar, CHECK'ler `NOT VALID`.
- **Kayıt asla kaybolmaz** (§4.6): kısıtlar ikinci emniyettir; `want` asla 400 değildir; talep kartının
  hiçbir hatası yazılmış dokunuşu ya da sonuç ekranını etkilemez; hiçbir çıkış sayaçsız kalmaz.
- **Tenant izolasyonu** (§4.5): §6 beşlisi; açık `tenant_id`; holding'de zincir, kademe, talep işverenin.
- **GPS** (§4.2): yalnız `tap.js`'in tek `getCurrentPosition` çağrısı.
- **Log** (§7): token, imzalı bağlam, e-posta yok; talepler opak id'lerle.
- **Tap/sonuç ekranı** (§9, skill `tappa-brand`): yalnız 2026-10-21 onayının iki değişikliği.

## Migration ve ADR numaraları (önceden dağıtıldı)

| No | Kart | İçerik | `Down` |
|---|---|---|---|
| **00064** | M15-02 | `tenants.day_cutoff` (NOT NULL DEFAULT `'05:00'`), `locations.day_cutoff`, `departments.day_cutoff`, `employees.day_cutoff` (NULL = devral), `transactions.business_day_start` + CHECK `NOT VALID`, çıkış sınıflandırmasının SQL fonksiyonu | dolu bir `business_day_start` varsa **reddeder**; kesim sütunları düşer |
| **00065** | M15-09 | `transactions.direction_source` + CHECK `NOT VALID` | dolu bir değer varsa **reddeder** |
| **00066** | M15-09 | iki CHECK'in `VALIDATE`'i (`00044`'ün deseni — `SHARE UPDATE EXCLUSIVE`, tap'in INSERT'ini bekletmez) | `NOT VALID` hâline döner |
| **00067** | M15-15 | `punch_claims` + `punch_claim_decisions` (append-only, §6 beşlisi, TRUNCATE tetikleyicileri) | satır varsa **reddeder** |
| **00068** | M15-13 (koşullu) | `/v1` punch olayına `direction_source` (Q31(a) *evet* ise) | dolu bir değer varsa **reddeder** |

**Çakışma — ölçüldü (2026-10-21):** ADR 0031 M14'ün dalında alınmış; M14 migration numarası **ayırmıyor**
(`000NN` yer tutucusu), hiçbir dalda `00064`+ yok. M14'ün kodu M15'ten önce `main`'e girerse M15 kendi
migration'larını yeniden numaralar (aynı commit'te bütün atıflar); yayımlanmış bir migration'ın numarası
asla değişmez. M14'ün dalı `roadmap.md`'ye bir satır ekliyor — metin düzeyinde birleştirme çakışması.

## Görev listesi

| ID | Faz | Görev | Araç | Mercek | Bağımlılık |
|---|---|---|---|---|---|
| M15-01 | A0 | ADR 0032'nin kabulü + Q30–Q33 | ana oturum + kullanıcı | iki | — |
| M15-02 | A | Şema 00064 (dört kademe, `business_day_start`, sınıflandırma SQL fonksiyonu); etkin kesim okuması; `GetLastOpenTransaction` `@at` + `id` **ve çağıranı** | agent `tappa-db-migrator` | iki | M15-01 |
| M15-03 | A | Tap motoru: iş günü kuralı, `S`/`Next`, sınıflandırma Go fonksiyonu, notlar; ters iddiayla yeniden yazılan motor **ve DB** testleri; CLAUDE.md §5 | yapıcı | iki | M15-02 |
| M15-04 | A | Orkestratör: kesimi topla, iş günü anını yaz, NULL zorlaması; **saate bağlı DB testleri** | yapıcı | iki | M15-02, M15-03 |
| M15-05 | A | Tek sınıflandırma her okuyucuda (liste, maske sayısı, ledger, şerit; (b)/(c) geçici `StartedEarlier`); parite testi | yapıcı + agent `tappa-db-migrator` | iki | M15-04 |
| M15-06 | A | Yetim çıkış: `OutWithoutIn` + *"giriş eksik"* listesi; ADR 0011 S dolu varyantları | yapıcı + agent `tappa-db-migrator` | iki | M15-05 |
| M15-07 | A | Panel: dört kademe ayarı (Account / şube / departman / **yeni kişi eylemi**), çakışma uyarısı + öneri + açık giriş sayısı, `audit_log` | yapıcı (`tappa-brand`) | iki | M15-02, M15-03 |
| M15-08 | A | Faz A kapanışı (**Faz A'nın ayrı sevk kapısı**): seed, tam doğrulama, owner bağlamında çakışma raporu, **yayın günü panel duyurusu** | denetçi + güvenlik | iki | M15-02…05, M15-07 |
| M15-09 | B | Şema 00065 (`direction_source`) + 00066 (iki CHECK'in doğrulanması) | agent `tappa-db-migrator` | iki | M15-08 |
| M15-10 | B | Tap motoru: istenen yön, `DirectionSource`, `PredictDirection`, iki dilli not; CLAUDE.md §5 ikinci paragraf | yapıcı | iki | M15-09 |
| M15-11 | B | `POST /api/checkin`: `want` (asla 400), kilit altında karar, yazım | yapıcı | iki | M15-06, M15-10 |
| M15-12 | B | `GET /t` tahmini (**kapılı**) + tap ekranı (§9) + `tap.js` + Account önizlemesi + skill/ADR 0023 notları | yapıcı (`tappa-brand`) | iki | M15-11 |
| M15-13 | B | **Koşullu:** `/v1` punch akışına `directionSource` (00068) — Q31(a) *evet* ise Faz B'nin sevk kapısı | yapıcı + agent `tappa-db-migrator` | iki | M15-11 |
| M15-14 | B | Faz B kapanışı (+ M15-06'nın güvenlik denetimi, gerçek tarayıcıda `want`, anahtar canlıda) | denetçi + güvenlik | iki | M15-06, M15-09…13 |
| M15-15 | C | Şema 00067: `punch_claims` + `punch_claim_decisions` + RLS + TRUNCATE testi | agent `tappa-db-migrator` | iki | M15-14 |
| M15-16 | C | Domain: pencere (18 sa kırpılmış, açık uçlar), K3 bandı, K4 gösterim kuralı, yeniden doğrulama, askı | yapıcı | iki | M15-15 |
| M15-17 | C | Sonuç ekranı talep kartı + `POST /api/claim`; CLAUDE.md §5 *"Unutulan çıkış"*, §9; skill sonuç ekranı | yapıcı (`tappa-brand`) | iki | M15-16 |
| M15-18 | C | Panel: talep kuyruğu (odak deseni, K3 işaretleri, bant dışı onay adımı, toplu onay yok) | yapıcı (`tappa-brand`) | iki | M15-16 |
| M15-19 | C | Son kapanış: tam doğrulama, deploy notu (toplu geçiş) | denetçi + güvenlik | iki | hepsi |

**Önceki sürümden (`21bf93d`) eşleme:** 01→01 · 02→02 · 03→03 · 04→04 · 05→05 · 06 (panel ayarları)→**07** ·
07 (A kapanışı)→**08** · 08 (`direction_source` şeması)→**09** · 09 (yetim çıkış)→**06** (Faz A'ya taşındı:
yetim sınıfları `direction_source`'a değil yalnız S'ye bağlıdır ve Faz A'da da oluşur — M-Y1) · 10→10 ·
11→11 · 12→12 · 13 (panel/rapor görünürlüğü)→**ertelendi** (çalışanın seçimi fişin notunda zaten görünür;
rapor sayacı ve CSV sütunu M15 dışı — ürün merceği Ü-Dilim) · **13 yeni:** koşullu API kartı · 14→14 ·
15–19 aynı. Ürün merceğinin *"08 + 09 birleştir"* önerisi bu biçimde uygulandı: yetim çıkış şemaya değil
sınıflandırmaya bağlı olduğu için şema kartından ayrılıp Faz A'ya geçti; sıra (sınıflandırma → yetim →
`POST` → arayüz) korunur.

**Saat şişirme yolunun sırası (ADR 0032 §B.5).** Yolu açan `POST`'un `want` alanıdır (M15-11). Onu
kapatan sınıflandırma (M15-05) ve yetim çıkışın sayacı/listesi (M15-06) Faz A'dadır ve M15-11'in
bağımlılığıdır.

---

## Faz A0 — Karar

## M15-01 — ADR 0032'nin kabulü

- **Bağımlılık:** —
- **Kırmızı çizgi:** §4.3, §4.6, §4.5, §4.2 (ADR'nin analizi), §5, §9
- **Araç:** ana oturum; kullanıcı onayı · **Mercek: iki**
- **Commit:** `docs(adr): accept the business day and the direction chain`

**Amaç.** ADR 0032'yi *Kabul edildi*'ye geçirmek; Q30 (kesim değerleri), Q31 (`/v1` yön kaynağı — Faz B
kapısı), Q32 (talep sonucu bildirimi), Q33 (sonuç başlığının kelimesi) ve **"Faz A, M15-08 kapanınca
ayrıca canlıya çıksın mı?"** — **cevaplandı (K6, 2026-10-21): evet**, B ve C'yi beklemeden; B + C toplu
geçişle.

**Kabul kriterleri.**
- ADR'nin durum satırı *Kabul edildi (tarih)*; Q30–Q33 *cevaplandı* ya da bilinçli olarak *açık* (hangi
  kartı bloklar yazılı); Faz A'nın ayrı sevki için kullanıcının cevabı `state.md`'de ve bu dosyanın
  başındaki *"Sevk"* notunda aynı cümleyle.
- `TestEveryNamedTestExists` yeşil.
- CLAUDE.md **bu kartta değişmez** — her normatif paragraf onu uygulayan kodla aynı commit'te:

  | CLAUDE.md paragrafı | Kart |
  |---|---|
  | §5 *"Yön (in/out)"* — iş günü kuralı ve kademe | M15-03 |
  | §5 *"Yön (in/out)"* — çalışanın seçimi ve raporun eşlemesi | M15-10 |
  | §5 *"Unutulan çıkış"* — talep cümlesi | M15-17 |
  | §9 tap ekranı — tahmini yön + düzeltme bağlantısı | M15-12 |
  | §9 onay ekranı — talep kartı istisnası | M15-17 |
  | §3 dizin haritası — `internal/domain/claim/` | M15-16 |

---

## Faz A — İş günü (çığı keser)

## M15-02 — Şema: dört kademe, iş günü anı, sınıflandırma fonksiyonu (00064)

- **Bağımlılık:** M15-01
- **Kırmızı çizgi:** §4.3 (sütun, geri doldurma yok, `Down` kanıt silmez), §4.5 (kolon yetkileri), §4.6 (kısıt kaybı), §6
- **Araç:** agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(db): add the business day cutoff and freeze the business day of each tap`

**Amaç.** Kademenin dört sütunu, `transactions.business_day_start`, çıkış sınıflandırmasının SQL
fonksiyonu, tap okumasında etkin kesim, `GetLastOpenTransaction`'ın `@at` sınırı ve `id` beraberlik
bozucusu — **ve bu sorgunun çağıranı** (sqlc'nin `At time.Time` alanı sıfır değerle kalırsa her dokunuş
`in` olur; motor merceği M-O2).

**Dokunulacak dosyalar.** `db/migrations/00064_add_the_business_day.sql`, `db/queries/employees.sql`
(`GetEmployeeForTap`), `db/queries/transactions.sql` (`GetLastOpenTransaction`, `InsertTransaction` ve
`store.Transaction`'ı tam döndüren her sorgu — `GetLastTransactionForEmployee` dahil), ayar yazma
sorguları (`tenants.sql`, `locations.sql`, `departments.sql`, `employees.sql`), `internal/store/*`
(üretim), `internal/domain/checkin/checkin.go` (yalnız `gatherPerson`'ın `GetLastOpenTransaction`
çağrısı: `At` = `Record`'un `occurredAt`'i, `gatherWith`'e bir parametre), DB testleri.

**Adımlar.**
1. Her `ALTER TABLE` dosyanın ilk ifadesi `SET LOCAL lock_timeout = '1s'` (`00037:52`, `00038:39`).
2. `tenants.day_cutoff time NOT NULL DEFAULT '05:00'`; `locations.day_cutoff`, `departments.day_cutoff`,
   `employees.day_cutoff` (`time`, NULL = devral). Dördünde de `CHECK (date_part('second', day_cutoff) =
   0 AND day_cutoff < '24:00')` — `'24:00'::time` Postgres'te geçerlidir ve gün başlangıcıyla aynı ana
   düşerdi (motor merceği M-DÜŞÜK 3).
3. **Kolon yetkileri:** `tenants` UPDATE kolon kolon verilmiştir (`00024:120` — `name, business_type,
   timezone`; `:135` `Down`'dır) → `GRANT UPDATE (day_cutoff) ON tenants TO tappa_app`. `locations`,
   `departments`, `employees` tablo düzeyinde UPDATE'e sahip (`00002:102`, `00003:105`; ölçüldü:
   sonraki hiçbir migration daraltmıyor) — ek GRANT yok.
4. `transactions.business_day_start timestamptz` (NULL) ve `CHECK (business_day_start IS NULL OR type IS
   NOT NULL) NOT VALID` — doğrulaması 00066'da (`00044`'ün deseni: düz `ADD CONSTRAINT CHECK` ~2 M satırda
   ~195 ms `ACCESS EXCLUSIVE` altında tam tarama yapar — motor merceğinin ölçümü; `ADD COLUMN` meta veri
   işlemidir, CHECK değildir). Bu kısıt **ikinci emniyettir**; birincisi `insertParams` (M15-04).
5. **Çıkış sınıflandırması** (ADR 0032 §A.6): tek fonksiyon (ad yapıcının; ör.
   `tappa_checkout_class(prev_in_at timestamptz, prev_is_out boolean, day_has_in boolean,
   prev_written_later boolean, out_at timestamptz, out_day_start timestamptz) RETURNS text`) —
   `LANGUAGE sql IMMUTABLE`, `SECURITY INVOKER`, **tablo okumaz**; altı değer: `closes`, `too_long` (a),
   `other_day` (b), `orphan` (c), `extra` (e), `unanchored` (d). Sıra: önsüz mü (`prev_in_at` NULL → S NULL
   ise d; `prev_is_out` **ve** `day_has_in` **ve** `prev_written_later` ise e; değilse c), sonra S koşulu
   (düşerse b — a ∧ b → b), sonra 18 sa (`closes` / `too_long`). `P`, `I` (çıkıştan **önce**,
   `(occurred_at, id)` sırasıyla, `occurred_at ≥ S` bir `in`) ve `W` (`P.created_at > out.created_at`)
   çağıran sorguda alt sorgularla bulunur (ADR 0032 §A.6).
   **Yetki (ölçüldü):** `REVOKE EXECUTE … FROM PUBLIC` (`00061:98-102`) + `GRANT EXECUTE … TO tappa_app`
   (`00003:216-217`, `00004:182-183`); `op_`/`holding_` öneki taşımadığı ve `SECURITY INVOKER` olduğu için
   `internal/db/operatorschema_test.go:501-517`'nin katalog kontrollerine girmez. Süre literali
   `tap.StaleOpenIn`'in kopyasıdır; M15-05'in parite testi bağlar.
6. `GetEmployeeForTap`: `LEFT JOIN departments d ON d.id = e.department_id AND d.tenant_id = e.tenant_id`,
   `LEFT JOIN locations l ON l.id = e.location_id AND l.tenant_id = e.tenant_id` (profil şubesi; `e.location_id`
   NULL olabilir — `00037:88`) ve `coalesce(e.day_cutoff, d.day_cutoff, l.day_cutoff, t.day_cutoff) AS
   effective_day_cutoff`.
7. `GetLastOpenTransaction`: `@at` — dış satırda `t.occurred_at <= @at`; `NOT EXISTS` içinde `(o.occurred_at,
   o.id) > (t.occurred_at, t.id) AND o.occurred_at <= @at`; `ORDER BY t.occurred_at DESC, t.id DESC`
   (motor merceği M-O3: bugün katı `>` ve `id`'siz; sınıflandırma ve ledger `(occurred_at, id)` sırasıyla
   okur — `transactions.sql:893`). Yorum bloğu korunur; yeni paragraf `@at`'in ve beraberlik bozucusunun
   nedenini yazar. **Çağıran aynı commit'te:** `gatherPerson` `At`'i `Record`'un `occurredAt`'inden alır.
8. `InsertTransaction`: `business_day_start` parametresi. Eş mekân dokunuşu da bu sorguyu kullanır
   (`checkin.go:1559-1568`); *"site kardeşi"* yalnız manuel yazımdadır (`InsertManualSiteTransaction`,
   `transactions.sql:393`) ve S yazmaz. `store.Transaction` sütun listesi değişince onu tam döndüren her
   sorgu (`GetLastTransactionForEmployee`, `RETURNING` listeleri) güncellenir — yoksa sqlc derlemesi kırılır.
9. **`Down`:** dolu bir `business_day_start` varsa `RAISE EXCEPTION … USING ERRCODE =
   'object_not_in_prerequisite_state'` (`00043:430-433`); yoksa fonksiyonu, GRANT'ı, kısıtı ve beş sütunu
   düşürür.

**Kabul kriterleri.**
- `Down` boş veritabanında geri alır, dolu bir `business_day_start` varken reddeder ve hiçbir şey değiştirmez.
- `tappa_app` kendi tenant'ının `day_cutoff`'unu günceller; `00024`'ün kapalı tuttuğu kolonlar kapalı.
- `'24:00'` ve saniyeli bir değer reddedilir.
- `GetEmployeeForTap` kademe testi: kişi > departman > şube > işletme; profil şubesi NULL bir çalışanda
  şube atlanır; departmanın şubesi profil şubesinden farklıysa departman kazanır.
- `GetLastOpenTransaction`: `@at = now` ile mevcut DB testleri (`TestGatherDB_` önekli olanlar) değişmeden
  yeşil; geçmiş `@at` o an açık girişi döndürür; **aynı `occurred_at`'te iki satır** `id` sırasıyla
  sınıflandırmayla aynı cevabı verir.
- Sınıflandırma fonksiyonunun altı sınıfı ve sınırları birer DB vakasıyla (parite M15-05'te).
- **Kart kendi commit'inde yeşil:** `go build ./...` ve `-run` ile değişen testler; çağıran güncellenmeden
  sorgu sevk edilmez.
- `bash scripts/redline-check.sh` yeşil.

**Tuzaklar.** `tenants` için tablo düzeyinde `GRANT UPDATE` · CHECK'i doğrulamalı eklemek · `@at`'i yalnız
dış satıra koymak · `id` beraberlik bozucusunu yalnız `ORDER BY`'a koymak · çağıranı sonraki karta bırakmak
· koşulsuz `Down`.

## M15-03 — Tap motoru: iş günü kuralı ve sınıflandırma

- **Bağımlılık:** M15-02
- **Kırmızı çizgi:** §5 (normatif metin), §4.6, CLAUDE.md §8 (%90+)
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(tap): close an open check-in only within its business day`

**Amaç.** `resolveDirection`'ı ADR 0032 §A.1'e geçirmek; iş günü sınırının ve çıkış sınıflandırmasının saf
Go fonksiyonlarını dışa açmak.

**Dokunulacak dosyalar.** `internal/domain/tap/types.go`, `decide.go`, `decide_test.go`,
`crosslocation_test.go`, `table_test.go`, yeni test dosyaları, `internal/handler/checkin_db_test.go`
(alt test, aşağıda), `CLAUDE.md` (§5 ilk paragraf), `internal/domain/ledger/site.go` (`partnerKeptAnnotations`).

**Adımlar.**
1. `type DayCutoff struct { Cutoff time.Duration; Timezone string }`, `Input.DayCutoff *DayCutoff` (çözülmüş
   kesim). `nil` = yalnız `StaleOpenIn`.
2. **`S(at)`** (ad yapıcının; ADR 0032 §A.3): `at`'in yerel tarihi `d` için **üç aday** — `d − 1`, `d`,
   `d + 1`; her adayın tarihi **önce normalize edilir** (`time.Date(y, m, d±1, 12, 0, 0, 0, loc)`'un
   tarihi), sonra o tarihte kesimin duvar saati kurulur; `S` = `at`'ten sonra olmayan **en geç** aday.
   **Sonbaharın iki kez yaşanan saati** açık kodla: aday kurulurken aynı duvar saatini veren daha erken bir
   an denenir, **erken** olan seçilir (Go sonraki geçişi seçer). İlkbaharda var olmayan saat `time.Date`'in
   normalize ettiği ana düşer, pinlenir. Bölge boş/yüklenemez → `ok=false`. *(4. sürümün "o günün adayı,
   sonraysa bir gün geri" kuralı Santiago/Havana'da ölçümde düştü — `S(S) ≠ S` 3366, `at ≥ Next` 90 ihlal;
   gece yarısında var olmayan 00:00'ı Go geriye normalize ediyor.)*
3. **`Next(S) = S(S + 26 sa)`** — yerel tarihe gün eklemek değil.
4. **Çıkış sınıflandırması** (ADR 0032 §A.6): `(prev *Prev, dayHasIn bool, outAt time.Time,
   outDayStart *time.Time) CheckoutClass` — `Prev` önceki yönlü olayın yönü, anı ve *"çıkıştan sonra
   yazıldı"* bilgisi (`W`); altı sınıf, SQL fonksiyonuyla aynı sıra; *"kapatır"* =
   `== closes`. Motor onu çağırmaz (motorun sorusu *"kapatmalı mı"*), ama tutarlıdır: motorun `out`
   dediği her durumda sınıf `closes`'tur (özellik testi).
5. `resolveDirection` — ADR 0032 §A.1 tablosu; `at := tapInstant(in)` (bugün `in.Now`); `Decision`'a iş
   günü anı (`*time.Time`, yalnız yönlü kararlarda) ve açık bırakılan giriş (`id` + neden: önceki iş günü |
   eski).
6. **İki dilli not** (ADR 0032 §B.4): kaydın notu *"previous check-in left open (no checkout recorded)"*,
   `EmployeeNote` insan dilinde (*"Your previous clock-in has no clock-out."* — metinleri yapıcı/denetçi
   kesinleştirir). **Eski `StaleOpenInNote` sabiti kalır** (tarihsel notlar); yeni kayıt notu
   `partnerKeptAnnotations`'a eklenir.
7. `StaleOpenIn`, `resolveDirection` ve `Type` yorumları ADR 0032'ye atıfla yeniden yazılır.
8. **CLAUDE.md §5** *"Yön (in/out)"* ilk paragrafı ADR 0032'deki metinle (kademe dahil).

**Ters iddiayla yeniden yazılan testler** — yeni 18 sa kuralı **kesim yokken de** geçerlidir; şunlar
*"stale → `out`"* şeklini pinler ve **bu kartın commit'inde** kırmızıya döner, silinmez, ters iddiayla
yeniden yazılır:
- `decide_test.go:597-611` ve `:613-634`;
- `TestDecide_CrossLocationNoteSitsBetweenTheRuleAndTheDirection` (`crosslocation_test.go:197-211`) ve
  `TestDecide_TheEmployeesNoteHasNoNoteEffect` (`:213-237`) — not sırası yeni notla yeniden pinlenir;
- **`internal/handler/checkin_db_test.go:1808-1830`** *"ok out closing a stale check-in"* alt testi
  (motor merceği M-O2: motor değişikliği onu bu commit'te kırar) — 19 sa yaşında açık girişin ardından
  dokunuş `in` + yeni not; üretimden sürülen notun ekrana birebir çıkması iddiası korunur.
- Kırılmayanlar (motoru stale girişle çalıştırmıyorlar): `crosslocation_test.go:134`, `table_test.go`,
  `ledger/partnernote_test.go:36`, `ledger/holding_db_test.go:825-832`, `result_test.go:1018-1032`,
  `unbranded_golden_test.go:152`.

**Kabul kriterleri.** (`TestDecide_` önekli, tablo bazlı)
- Envanterdeki testler ters iddiayla yeniden yazıldı; geri kalan bütün `TestDecide_` testleri değişmeden
  yeşil (ör. `TestDecide_DirectionNilForNonRecordVerdicts`, `TestDecide_DirectionPracticeOpenInDoesNotCloseChain`).
- Kullanıcının vakası; *"17 sa"* vakası (Pzt 16:00 açık → Sal 09:00 → `in`; kesimsiz `out` — yan yana);
  Rusty Bar; fabrika gece vardiyası (05:00 ile `in`, departman 14:00 ile `out`); beyan edilmiş an; sıra dışı
  beyan.
- **Tüm bölgeler özellik testi:** on iki bölge — Europe/Malta, Asia/Beirut, America/Asuncion,
  Australia/Lord_Howe, Pacific/Chatham, America/Scoresbysund, Antarctica/Troll, Africa/Casablanca,
  America/St_Johns, Asia/Tehran, America/Santiago, America/Havana — × 15 dk'lık her kesim × 2025–2027,
  5 dk adımla: `S(at) ≤ at < Next(S(at))`, `S` monoton, `S(S) = S`, gün uzunluğu 22–26 sa. **0 ihlal** —
  motor merceğinin aynı algoritmayla ölçtüğü sonuç (`scratchpad/m15rev-engine/dst/v4`, 2026-10-21, tarihli
  gözlem). Santiago/Havana'nın kesim 00:00 vakası (`at` = 2025-09-06 23:00 −04) adıyla pinlenir.
- DST: Malta 2026-10-25 ve 2027-03-28'de 05:00 ve 02:30 kesimleri; sonbaharda iki kez yaşanan saatte
  **erken** an.
- Sınıflandırma: altı sınıfın her hücresi + sınırlar (`in.At == S` kapatır; tam 18 sa kapatır; 18 sa + 1 sn
  (a); a ∧ b → b; `dayHasIn` ile (c)/(e) ayrımı).
- Saflık; paket kapsamı ≥ %90; **kart kendi commit'inde yeşil** (handler DB alt testi dahil).

**Tuzaklar.** `in.Now`'ı bırakmak · `Next`'i *"tarih + 1"* ile kurmak · `S`'yi tek adayla kurmak (Santiago,
Havana) · Go'nun sonbahar seçimine güvenmek · **"erken olanı seç" karşılaştırmasını `d − 1`'in normalize
edilmemiş tarihiyle yapmak** (ölçüldü: Malta 02:00 kesiminde `S(S) ≠ S` 114 kez — önce tarih normalize
edilir) · eski not sabitini silmek · DB alt testini M15-04'e bırakmak.

## M15-04 — Orkestratör: kesimi topla, iş günü anını yaz; saatten bağımsız testler

- **Bağımlılık:** M15-02, M15-03
- **Kırmızı çizgi:** §4.6, §4.5 (holding), §4.3
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(checkin): judge the direction against the person's business day`

**Amaç.** `checkin` etkin kesimi ve saat dilimini `Input.DayCutoff`'a koyar, kararın iş günü anını yazar;
saate bağlı DB testlerini saatten bağımsız yapar.

**Adımlar.**
1. `gatherWith`: `emp.EffectiveDayCutoff` + `emp.TenantTimezone`. **Eş mekânda da işverenin** kademesi
   (profil şubesi işverenin şubesi). `site.Timezone` yalnız vardiya içindir.
2. `insertParams`: `dec.Type != nil` **ve** kanal `nfc`/`qr` ise S yazılır; **yönsüz kararda ve manuel
   kanalda** (`checkin.Record` hâlâ `ChannelManual` kabul ediyor — `checkin.go:1102`) kodda NULL. CHECK
   ikinci emniyettir.
3. `ledger/report.go:131-169` yorum bloğu: motor artık kayıt anını ölçüyor (yalnız bu kartta).
3b. **Yayın anahtarı** `TAPPA_BUSINESS_DAY_FROM` (RFC 3339 an; ADR 0032 §A.8). **Etkin** ⇔ değer var ve
   **sunucunun** `Now`'ı ≥ değer (beyan edilen anla değil — güvenlik merceği F-2). **Tek kapı fonksiyonu
   saftır ve `tap` paketindedir** (ad yapıcının; biçim `(from *time.Time, now time.Time, cutoff time.Duration,
   tz string) → *DayCutoff` — anahtar etkin değilse nil) ve hem `gatherWith`'te hem M15-12'nin `GET /t`
   tahmininde çağrılır (motor merceği ORTA-2, N4). Anahtarın değeri **hem `checkin.Service`'e hem
   `tenant.Directory`'ye** yapı alanı olarak enjekte edilir; `tenant` ile `checkin` arasında **yeni bir import
   bağı kurulmaz** (ölçüldü: bugün ikisi birbirini içe aktarmıyor; ikisi de `tap`'i içe aktarabilir). Etkin değilken `DayCutoff: nil` (M15-03'ün nil davranışı — yalnız 18 sa
   kuralı, hiçbir vardiyayı kesmez), S yazılmaz, Faz B yüzeyleri kapalı. Değer **`checkin.Service`'e yapı
   alanı olarak enjekte edilir** (CLAUDE.md §7; paket düzeyi durum yok); `internal/config` ayrıştırır:
   - biçim hatası başlangıçta reddedilir (`config.go:278-284`'ün `TAPPA_LOG_FORMAT` kuralı);
   - üst sınır: başlangıçta *"şimdiden en çok 30 gün sonrası"*, ötesi red (`2062` yazım hatası sessizce
     *"hiç etkin olmaz"* olmasın); geçmişteki değer her zaman geçerli;
   - değer başlangıçta loglanır (sır değil);
   - tek pod çalışan kümede (`deploy/k8s/20-app.yaml:53` `replicas: 1`) bir yazım hatası bir yeniden
     başlatmada hiçbir dokunuşun kaydedilmemesi demektir → `cmd/tappa`'nın `configMapValues` yardımcısıyla
     `deploy/k8s/05-config.yaml`'daki değerin (varsa) ayrıştırıldığını ve sınırın içinde olduğunu pinleyen bir
     paketleme testi (güvenlik merceği F-3).
   **Geri alma:** anahtarı kaldırmak yeni dokunuşları kesimsiz kurala döndürür; donmuş S'ler kalır.
4. **Saate bağlı DB testleri** (motor merceği M-Y2). 00064'ün DEFAULT 05:00'ı, göreli saatle kurulmuş
   testleri günün belirli saatlerinde kırmızıya çevirir — ölçülenler:
   `TestSeedDB_APracticeRowNeverHidesAnOlderOpenCheckIn` (`internal/handler/practice_direction_db_test.go:113`;
   `now − 3 sa` giriş → Malta 05–08 arası), `TestSeedDB_ASecondActivationWritesNoRecordAndLeavesTheChainAlone`
   (aynı dosya `:163`, `:181`; 07–10 arası), `TestHoldingTap_DirectionChainStaysAtTheEmployer`
   (`internal/domain/checkin/holding_db_test.go:530-553`; `now − 5 dk` → 05:00–05:05).
   - Tam envanter `grep` ile çıkarılır (göreli saatler: `now.Add(-…)`, `interval '… hours|minutes'`,
     `AgedBy`, `seedTapAgedBy`; ilk ölçüm 13 dosya — `checkin_db_test.go`, `e2e_db_test.go`, `qr_db_test.go`
     dahil) ve sınıflandırılır: yön zinciri olanlar düzeltilir.
   - Düzeltme: **kişi kademesine** yazan tek bir yardımcı — testin kendi işe aldığı çalışana (`f.hire`)
     duvar saatinden uzak bir kesim verir (ör. yerel `now + 12 sa`, 15 dk'ya yuvarlanmış); testler kendi
     senaryolarının iş günü içinde kalır. **Paylaşılan seed tenant'ına ya da şubesine yazmaz** (motor merceği:
     `fixtures.TenantKF` birçok harness'ın tenant'ıdır — `internal/handler/seedflow_db_test.go:332`; tenant ya da
     şube kesimini değiştirmek paralel testleri ve bu kartın kendi kriterlerini bozar ve dev veritabanında
     kalıcı iz bırakır). Kesimi ya da kademeyi bilerek sınayan testler **kendi tenant'ını / şubesini** kurar.

**Kabul kriterleri.**
- DB testi, **kendi kurduğu** bir tenant (işletme kesimi 05:00): Pzt açık giriş, Sal sabah `in`,
  `business_day_start` Sal 05:00 yerel.
- Rusty Bar gecesi uçtan uca (kendi tenant'ında Rusty Bar biçimli bir şube): iki satırda aynı S.
- Kademe uçtan uca (kendi tenant'ında iki şube): A şubesinin kesimi (06:00) A profilli çalışana B
  plaketinde de uygulanır (profil şubesi, dokunulan değil); profil şubesi NULL (API çalışanı) → işletme kesimi.
- Holding: KM çalışanı KF plaketinde KM'nin kademesi; `TestHoldingTap_DirectionChainStaysAtTheEmployer` yeşil.
- `TestSeedDB_ADayAtKFStJulians`, `TestGatherDB_APracticeRowDoesNotHideAnOlderOpenCheckIn` yeşil.
- Yönsüz kararda ve manuel kanalda S NULL — birim testiyle `insertParams` düzeyinde, DB testiyle satırda.
- **Anahtarın modları (motor merceği ORTA-1):** harness'lar ve `checkin.Service` testlerde değeri **uzak
  geçmişe** koyar (üretim modu — bütün yön testleri bu modda). *"Değer yok"* ve *"sunucu `Now`'ı değerden
  önce"* modları **adlandırılmış ayrı testlerdir**: ikisinde de `DayCutoff` nil, S NULL, kesimle çakışan bir
  vardiya (04:00–12:00) doğru yön alır. **Beyan edilen an değerden sonra, sunucu `Now`'ı önce** → kapalı
  (F-2). Config testleri: biçim hatası ve 30 gün üstü red, geçmiş değer kabul, değer log satırında.
- Paketleme testi `05-config.yaml`'ın değerini ayrıştırır (değer yoksa testin kendisi bunu söyler, geçer).
- **Suite saatten bağımsız — yardımcının özelliğiyle kanıtlanır.** İki sabit duvar saatiyle koşmak, satırını
  veritabanının `now()`'ıyla kuran testlerde uygulanamaz (ör. `internal/domain/checkin/holding_db_test.go:516`:
  `now() - $3::interval`). Bunun yerine: (1) yardımcının saf bir birim testi — herhangi bir `now` için
  verdiği kesimin iş günü sınırı `[now − 12 sa, now + 6 sa]` aralığına **düşmez** (24 saatin her 5 dk'sı
  için); (2) envanterdeki her yön-zinciri testinin bu yardımcıyı ya da kendi açık kesimini kullandığını
  iddia eden kaynak düzeyi bir test (envanter listesi testin içinde, adlarıyla).
- Mutasyon: `@at`'i `now`'a sabitlemek, kesimi yutmak, NULL zorlamasını silmek — her biri bir testi kırar.

**Tuzaklar.** Paylaşılan seed tenant'ının ya da şubesinin kesimini değiştirmek · eş mekânda sahanın kesimini
okumak · dokunulan şubenin kesimini okumak · saate bağlı testi
`t.Skip` ile susturmak.

## M15-05 — Tek sınıflandırma her okuyucuda

- **Bağımlılık:** M15-04
- **Kırmızı çizgi:** §4.6 (sinyal kaybı, sayaçsız çıkış), bordro, §4.5
- **Araç:** yapıcı (ledger, şerit) + agent `tappa-db-migrator` (sorgular) · **Mercek: iki**
- **Commit:** `fix(ledger): classify a checkout by one rule everywhere`

**Amaç.** ADR 0032 §A.6'nın sınıflandırması **her** okuyucuda: `ListOpenCheckIns`, `CountMaskedOpenCheckIns`,
`accumulate`, şeridin eşlemesi (`tape.go:178`). Talep kapısı (M15-16) ve *"giriş eksik"* listesi (M15-06)
aynı sınıflandırmayı kullanır.

**Adımlar.**
1. `ListOpenCheckIns`: bir `in` açık ⇔ sonraki ilk yönlü, practice olmayan olay (`(occurred_at, id)`) yok,
   `in`, ya da sınıfı `closes` olmayan bir `out` (SQL fonksiyonu). Açık `tenant_id` filtresi her alt sorguda.
2. `CountMaskedOpenCheckIns`: maskelenmiş girişler listede görünür; sorgu ve ekran cümlesi yeniden yazılır
   ya da emekliye ayrılır.
3. `accumulate`: sınıf → dal: `closes` → süre; (a) → `TooLong` + `openEntry(in)`; (d) ve (e) →
   `StartedEarlier`; (b) → `openEntry(in)` ve açık slot **boşalır** (`open = nil`); **(b) ve (c)'nin çıkış
   yarısı bu kartta geçici olarak `StartedEarlier`** (hiçbir çıkış sayaçsız kalmaz; M15-06 onu
   `OutWithoutIn`'e taşır). 5. sürümün (e) kuralıyla (`I` ∧ `W`) motorun çıkışı Faz A'da hiç (b)/(c) olmaz —
   geçici eşleme yalnız fixture, manuel ya da kuyruk satırlarıyla ulaşılır (bir özellik testi pinler: rastgele
   motor zincirleri + rastgele sonradan eklenmiş manuel düzeltmeler → hiçbir motor `out`'u (b)/(c) değil).
   `ListWorkedShiftEvents` ve şeridin okuması `created_at`'i döndürür; ledger kişi başına son `in`'i (`I`,
   sırayla) ve önceki olayın `created_at`'ini (`W`) izler. Dönem sınırı ADR 0032 §A.6'daki gibi.
4. Yorumlar: `anomalies.sql:647-652`'nin *"meşru fark"* bloğu yeniden yazılır; ekran cümlesi güncellenir.
5. **Parite testi** (DB): altı sınıfın her hücresi, sınırlar, **eşzamanlı iki satır** (aynı `occurred_at`,
   `id` sırası — `I`'nın sırayla tanımı dahil), `W` eşitliği (aynı `created_at` → (c)), **mola** ve
   **düzeltme** şekilleri Go ve SQL'de; SQL süresi `pg_get_functiondef`'ten okunup `tap.StaleOpenIn` ile
   karşılaştırılır (`TestGuardrails_TagNotActiveIsAnAllowList`'in deseni).
6. **`GetLastOpenTransaction` neden sınıflandırmayı kullanmaz** — özellik testi, **sabit kesimle** ve
   **kesim değişimi** vakasıyla (muhafazakâr sınır pinlenir: çıkış uydurulmaz, giriş listede kalır).
7. **Envanter (eski tanımı taşıyanlar, motor merceği M-DÜŞÜK 7):** `internal/domain/ledger/anomaly_test.go:412`
   (alan listesinde `ClosedLater`), `internal/handler/anomalies_test.go:136` (`MaskedOpenTally{StillOpen}`),
   `internal/handler/seedflow_db_test.go:766-772` (`openCheckIns` yardımcısı eski `NOT EXISTS` tanımını
   taşıyor) — yeni tanıma geçer ya da yardımcı sınıflandırma fonksiyonunu çağırır.
8. Maliyet: `EXPLAIN (ANALYZE, BUFFERS)` önce/sonra, tarihli gözlem.
9. **Manuel yazım kişi kilidini alır** (motor merceği DÜŞÜK-2; ADR 0032 §A.6 *"Eşzamanlı yazım"*):
   `internal/domain/manual`'ın yazım işleminin ilk ifadesi `LockEmployeeForTap` — bugün almıyor ve müdürün
   düzeltmesi motor çıkışıyla yarışınca `created_at` sırası ters dönüp (e) yerine (c) çıkabiliyordu. Kilit
   sırası tap yoluyla aynı (kişi → holding sayaçları, ADR 0027 §8); talep onayı (M15-16) kilidi zaten alır,
   `pg_advisory_xact_lock` aynı oturumda yeniden girişlidir.
10. **`created_at`'in tek yazarı veritabanı** (güvenlik merceği F-4): `transactions.created_at`'in yazılmadığını
   pinleyen kaynak düzeyi test; taradığı yerler (güvenlik merceği D-2): `db/queries`'teki her `INSERT INTO
   transactions` sütun listesi, **test dışı Go dosyalarındaki** ve **migration fonksiyon gövdelerindeki** her
   `INSERT INTO (public.)transactions` ifadesi (`internal/store` sqlc üretimidir, kaynağıyla birlikte geçer).
   Ölçüldü (2026-10-21): test dışı Go'da yalnız bir yorum (`internal/domain/checkin/policyset.go:26`) ve üretilmiş
   `internal/store/transactions.sql.go`; migration'larda hiç yok — test bugün yeşil başlar ve bir yazarın eklenmesini
   yakalar.
   Duvar saati tetikleyicisi (`00033:293-308`) seçilmedi: test fixture'ları `created_at`'i bilerek geçmişe
   yazar (`internal/domain/checkin/holding_db_test.go:516`) ve kırılırdı; kolon düzeyinde `INSERT` yetkisi her
   yeni sütunda GRANT bakımı isterdi.

**Kabul kriterleri.**
- Pzt `in`, Sal `in`, Sal `out` → Pzt listede.
- Üçüncü göz vakası (fixture S yazar): Pzt 15:00 `in`, Sal 07:00 `out` (S Sal 05:00) → (b): Pzt listede ve
  `OpenEntry`, 0 sa.
- **Açık slot:** Pzt 15:00 `in`, Sal 07:00 `out`, Sal 08:00 `out` → (b), (c); ledger ve SQL satır satır aynı.
- **Yönetici düzeltmesi (M-Y1):** motor 09:00 `in`, motor 20:00 `out` (S dolu), manuel 17:00 `out` → 8 sa,
  20:00 (e) → `StartedEarlier` 1 (bugünkü ADR 0011 sonucu), *"giriş eksik"*te yok.
- Tarihsel stale şekil (S NULL, 24 sa) → (a): `TooLong` 1, `StartedEarlier` bugünkü değerinde.
- **Tarihsel raporlar değişmez:** S'si NULL bir hafta için `accumulate` çıktısı bit bit aynı (eski ve yeni
  dallar aynı fixture'larda).
- Faz B şekli (fixture): 05:30 `in` → ertesi 04:30 `out` (23 sa, S koşulu sağlanıyor) → yalnız (a).
- **Liste = rapor:** aynı tenant ve yerel hafta için raporun `Open` girişleri listeyle aynı küme; ölçüm
  dışı: `ReportEventCap`, `ListOpenCheckIns`'in `LIMIT @row_limit`'i (`anomalies.sql:710`), ledger'ın
  bağlayamadığı satırlar.
- ADR 0011'in iki tablosu (`correction_test.go`) değişmeden yeşil; şerit ve rapor aynı aralıklar; RLS
  izolasyon testleri yeşil.
- Parite yeşil; SQL süresini 19 sa'e çeviren, a ∧ b sırasını (a)'ya çeviren, `day_has_in`'i yok sayan ve
  `prev_written_later`'ı yok sayan mutasyonlar kırmızı.
- Faz A özellik testi: hiçbir motor `out`'u (b)/(c) sınıfına düşmez (yukarıda adım 3).
- **Kilit:** manuel yazım ile motor çıkışı aynı kişi için eşzamanlı (`-race`) → düzeltme şekli her sırada (e);
  holding eş mekân satırında kilitlenme yok (iki sırada da tamamlanır).
- **`created_at` testi:** bir sorguya `created_at` sütunu ekleyen mutasyon kırmızı.

**Tuzaklar.** (e) testlerinde düzeltmeyi **`manual.Recorder` ile ya da `DEFAULT created_at` ile** yazmamak —
geçmişe yazılmış `created_at`'li doğrudan bir `INSERT` `W`'yi yanlış kurar ve test (e) yerine (c) görür (motor
merceği N3) · sıradaki olayı yalnız `out` arasında aramak · `id` beraberlik bozucusunu unutmak ·
sınıflandırmayı satır içi kopyalamak · (b)/(c)'yi sayaçsız bırakmak · `closingTail`'i silmek.

## M15-06 — Yetim çıkış: sayaç ve *"giriş eksik"* listesi

- **Bağımlılık:** M15-05
- **Kırmızı çizgi:** §4.6, bordro, ADR 0011, §4.5
- **Araç:** yapıcı (ledger) + agent `tappa-db-migrator` (sorgu) · **Mercek: iki**
- **Commit:** `feat(ledger): count a checkout that closes nothing`

**Amaç.** (b) ve (c)'yi `Totals.OutWithoutIn`'e (ad yapıcının) ve bir *"giriş eksik"* listesine bağlamak.
Bu kart **`direction_source`'a bağlı değildir** — yetim sınıfları S, `I` ve `W`'ye bağlıdır. Faz A'da motor
çıkışı (b)/(c) olmadığı için kart **Faz A'nın sevk kapısının dışındadır** (M15-08); ama Faz B'nin `want`
alanından (M15-11) önce biter — çalışanın seçtiği çıkış bu şekilleri günlük üretir.

**Adımlar.**
1. `accumulate`: (b), (c) → `OutWithoutIn` + kişi/gün işareti (M15-05'in geçici `StartedEarlier`'ı kalkar);
   dönem başından önce S'li yetim (b ya da c) → `StartedEarlier` dikişi. (a), (d), (e) dokunulmaz.
2. *"Giriş eksik"* sorgusu (`anomalies.sql`): sınıfı `other_day` ya da `orphan` olan çıkışlar — **S NULL hiçbir
   satır ve (e) yoktur**; açık `tenant_id`; pencere ve `LIMIT @row_limit` `ListOpenCheckIns`'in deseni.
   Yeni okuyucu `TestEvidenceReaders_EveryOneIsClassified`'a sınıflandırılır (motor merceği M-DÜŞÜK 6).
3. `correction_test.go`'ya **S dolu** varyantlar: ADR 0011'in dört düzeltme şekli ve *"kapatılamaz açık
   kayıt"* zinciri, motor satırları S dolu, düzeltmeler manuel (S NULL) → aynı sonuç ((e) sayesinde).
   `manual.CorrectionsOnlyShorten` yorumu ve onay ekranı cümlesi (e)'yi adlandıracak biçimde güncellenir —
   davranış değişmez.

**Kabul kriterleri.**
- (b): Pzt 15:00 `in` + Sal 07:00 `out` (S Sal 05:00) → `OutWithoutIn` 1, `TooLong` 0, `StartedEarlier` 0;
  Pzt *"çıkış eksik"*, Sal 07:00 *"giriş eksik"*.
- (a ∧ b): 24 sa, S dolu → (b).
- (c) S dönem içinde → `OutWithoutIn` 1; dönem başından önce → `StartedEarlier` 1.
- (e) yönetici düzeltmesi → `StartedEarlier`, listede yok; tarihsel stale şekil → (a), listede yok; (d) → listede yok.
- **Mola vakası (5. sürümün YÜKSEK'i):** 09:00 `in`, 12:00 `out`, 17:00 çalışanın seçtiği `out` (S dolu,
  `P` = 12:00 çıkışı, ondan **önce** yazılmış) → **(c)**: `OutWithoutIn` 1, `StartedEarlier` 0, 17:00
  *"giriş eksik"* listesinde ve giriş talebi çapası. **Mutasyon:** `W` koşulunu silmek (4. sürümün kuralı)
  bu vakayı (e)'ye çevirir ve test kırmızıya döner.
- **Faz C'nin kendi akışı:** Pzt 15:00 `in`, Sal 07:00 seçilmiş `out` (b), Pzt'nin çıkış talebi onaylanır
  (manuel 23:00, sonradan yazılmış) → Sal 07:00 hâlâ **(c)** (`I` yok — Sal'ın girişi yok); `I` koşulunu
  silen mutasyon onu (e)'ye çevirir ve kırmızıdır.
- **Geriye beyanla yer değiştirme:** mola vakasına 17:01'de `occurred_at` 16:59:59 beyanlı bir `out`
  eklenir → 17:00 (e), 16:59:59 **(c)** — listede hâlâ tam bir yetim çıkış.
- **Çift sayım yok — değişmez testi** (motor merceği M-DÜŞÜK 10): her fixture'da dönem içindeki her `out`
  tam bir yerde sayılır: `closes` çiftleri (`Worked`, `Awaiting` ya da `Refused` — kuyrukta bekleyen ya da
  reddedilmiş çiftler dahil, durumları ne olursa olsun bir kez) + `TooLong` + `OutWithoutIn` +
  `StartedEarlier` + *"başka dönemin girişi"* dalı (bugünkü `continue`) = dönem içi çıkış sayısı.
- S NULL bir hafta için çıktı M15-05 öncesiyle bit bit aynı; ADR 0011 tabloları S NULL **ve** S dolu
  varyantlarda yeşil; liste RLS ve açık filtreyle kapalı.

**Tuzaklar.** Yetim çıkışı iki sayaca yazmak · listede S/(e) süzgecini unutmak · (a ∧ b)'yi (a) saymak ·
`W`'yi `occurred_at` ile kurmak (`created_at` sunucunundur, `occurred_at` istemcinin beyanı) · mola, düzeltme ve
Faz C vakalarında sonradan gelen satırı `manual.Recorder` ya da `DEFAULT created_at` dışında, geçmişe yazılmış
`created_at`'le kurmak (`W` yanlış kurulur — motor merceği N3).

## M15-07 — Panel: dört kademe ayarı, çakışma uyarısı ve öneri

- **Bağımlılık:** M15-02, M15-03
- **Kırmızı çizgi:** §4.5 (yetki: işletme kesimi yalnız owner), §9 (panel), §7
- **Araç:** yapıcı, skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(panel): set the business day cutoff and warn when it cuts a shift`

**Amaç.** Dört ayar yüzeyi (K1):
- **İşletme:** Account (owner — `internal/handler/accountactions.go:96` `mayEditAccount`).
- **Şube:** şube formu (`venueSaveHref` yolu), boş = devral; alanın ipucu *"Empty — uses the business
  setting (05:00)"* (değer o anki işletme kesimi).
- **Departman:** departman formu (`departmentSaveHref`), boş = devral; ipucu *"Empty — each person uses
  their branch's or the business's setting"* (departman birden çok şubeden kişi taşıyabilir).
- **Kişi:** panelde *"çalışan formu"* yok — kişi eylemleri ayrı rotalardır (`internal/handler/dashboard.go:168-176`).
  **Yeni bir eylem** (POST, `mountWriting` ana grubu — `ProtectWriting`; kişi kartında küçük bir form).
  Kişi kartı etkin değeri ve **kaynağını** gösterir (*"05:00 — from Sliema"*, *"14:00 — from KM Meat
  Production"*, *"set for this person"*).

**Çakışma, öneri, açık girişler** (ADR 0032 §A.7):
1. Saf çakışma fonksiyonu (`internal/domain/tenant`): `(başlangıç − 1 sa, bitiş + 2 sa]` bandı (alt uç hariç,
   üst uç dahil; gece vardiyasında gece yarısını aşar). *"Kesimi kullanan vardiyalar"*: o kademeden devralan
   her çalışanın (1) çözülmüş vardiyası — departman vardiyası varsa o, yoksa **profil şubesinin** vardiyası —
   **ve** (2) son **28 günde** dokunduğu şubelerin vardiyaları (adlandırılmış sabit; okuma kişinin kendi
   `transactions` satırlarından, açık `tenant_id` filtreli). (2)'nin nedeni (motor merceği): Sliema profilli
   bir çalışan Rusty Bar'da 18:00–02:00 çalışıyorsa Sliema'ya 01:00 kesimi (1)'e göre uyarısızdır ama her
   gecesini böler.
2. **Öneri** (ürün merceği Ü-O7): bantların hiçbirine düşmeyen en geniş aralığın ortası, 15 dk'ya yuvarlanmış
   (*"Suggested: 23:00 — outside every shift"*); aralık yoksa söylenir. İlk vardiya girildiğinde (şube /
   departman kaydı) etkin kesim yeni vardiyayla çakışıyorsa aynı uyarı ve öneri o formda.
3. **Açık girişler:** şu an açık olan (ADR 0032 §A.6'nın tanımı) ve yeni kesimle **bölünecek** girişlerin
   sayısı — `B_eski = Next_eski(S_eski(in.At))`, `B_yeni = Next_yeni(S_yeni(in.At))`; bölünür ⇔ `B_yeni <
   B_eski ∧ B_yeni ≤ in.At + StaleOpenIn` (`≤`, çünkü kapatma da `≤`'dir — motor merceği N2; DÜŞÜK-1: 5. sürümün *"eski kesimde o aralıkta sınır
   yoksa"* formülü 05:00 → 14:00'te 12:00 girişini *"bölünmez"* sayıyordu; yeni sınır 14:00'tür ve 17:00
   dokunuşu `in` olur).
3a. **Durum satırı** (ürün merceği P1): Account, şube, departman ve kişi ekranlarında kesimin yanında —
   anahtar etkin değilken *"Not active yet — the business day starts applying when Taptime switches it on"*,
   etkinken *"Active since <tarih>"* (tarih anahtarın değeri, tenant saat diliminde). Owner'ın kaydettiği
   kesim anahtar gelene kadar etkisizdir ama ekran onu etkin göstermez.
3b. **Taşınma** (motor merceği): profil şubesi ya da departman değişince (`db/queries/employees.sql:802-806`,
   panelin taşıma eylemi `employeeMoveHref`) etkin kesim **sessizce** değişir. Taşıma ekranı aynı çakışma
   uyarısını ve eski → yeni etkin kesimi gösterir (*"Business day starts 05:00 → 23:00 after the move"*);
   uyarı taşımayı engellemez (onayla).
4. **Örnek cümle** (ürün merceği D6): *"05:00 falls inside KM Pastry & Bakery's shift (04:00–12:00): a 03:58
   clock-in would count for the previous day and the 12:05 clock-out would be recorded as a clock-in.
   Suggested: 23:00 — outside every shift. Changing it now would also split 3 check-ins that are open right
   now."*
5. Kayıt ikinci POST'ta (onay alanıyla) — panelin onay deseni (ör. `deactivateconfirm.go`); her değişiklik
   `audit_log`'a (eski/yeni değer, kapsam id'si).

**Kabul kriterleri.**
- Yetkiler bugünkü gibi; kişi eylemi `ProtectWriting` arkasında (askıda 403 — ADR 0025).
- Kesim 05:00 ile seed vardiyaları: Rusty Bar, KF 10–22, *KM Central Kitchen* 06–14, *KM General*,
  *KM Warehouse* → **yok**; *KM Meat Production*, *KM Pastry & Bakery*, fabrika 22–06 → **uyarı**;
  10–22 vardiyası ve kesim 00:00 → **uyarı** (üst uç dahil).
- Öneri: KM'nin beş departmanı için önerinin bantların dışında olduğu; KF için 05:00'in zaten bant dışı
  olduğu ölçülür.
- **Kendi kurduğu** departmansız bir tenant'ta bir şubeye kesim yazılınca o şubenin profilli çalışanlarının
  kartı *"from <şube>"* der (paylaşılan seed tenant'ına yazılmaz).
- **Gerçekte çalışılan şube:** A profilli çalışan son 28 günde B'de (18:00–02:00) dokunduysa A'ya 01:00 kesimi
  **uyarır**; 29 gün önceki dokunuş saymaz.
- **Taşınma:** etkin kesimi değiştiren bir taşıma uyarıyı ve eski → yeni değeri gösterir; değiştirmeyen
  taşımada uyarı yok.
- **Açık giriş sayısı:** 05:00 → 14:00 ve 12:00 girişi → **bölünür** (motor merceğinin vakası); 22:00 girişi
  için 05:00 → 01:00 → bölünür; 08:00 girişi için 05:00 → 23:00 → `B_yeni` (aynı gün 23:00) < min(ertesi
  05:00, ertesi 02:00) → **bölünür** (23:30 dokunuşu `in` olur); 08:00 girişi için 05:00 → 03:00 → `B_yeni`
  (ertesi 03:00) ≤ ertesi 02:00 değil → bölünmez. **Uç (`B_yeni == in.At + 18 sa`):** Pzt 11:00 girişi, kesim
  07:00 → 05:00: `B_eski` = Sal 07:00, `B_yeni` = Sal 05:00 = `in.At + 18 sa` → **bölünür** (tam Sal 05:00'teki
  dokunuş eski kuralla `out`, yeni kuralla `in`); `<` ile yazılmış bir formül bunu kaçırır — mutasyon testi.
- Durum satırı iki modda doğru (*"Not active yet"* / *"Active since"*) — render testi.
- Onaysız ilk POST hiçbir şey yazmaz; `audit_log` her kayıtta tam bir kez; başka tenant'ın id'si → tanımlı ret.

**Tuzaklar.** Dokunulan şubenin vardiyasını kullanmak · gece vardiyasını düz karşılaştırmak · uçları ters
kurmak · *"boş = 00:00"* okumak.

## M15-08 — Faz A kapanışı

- **Bağımlılık:** M15-02 … M15-05, M15-07 (M15-06 kapının dışında)
- **Kırmızı çizgi:** hepsi (faz diff'i)
- **Araç:** ayrı denetçi + agent `tappa-security-auditor`; skill `tappa-seed` · **Mercek: iki**
- **Commit:** `test(seed): give each demo business a business day` (+ yalnız düzeltme commit'leri)

**Adımlar.**
1. Seed: KF kesimi 05:00; KM kesimi **Q30'un cevabı** (öneri 23:00); `tags` satırlarına dokunulmaz.
2. Tam `make check`, `make audit`; güvenlik merceği Faz A diff'inde.
3. **Canlı çakışma raporu — tenant başına, owner'ın bağlamında** (güvenlik merceği: *"her tenant için"* bir
   rapor `tappa_app` RLS'i altında yapılamaz ve `op_*` dışında tenant sınırını aşmak §4.5 ihlalidir). Account
   ekranında owner'a açık, salt okunur bir *"Business day check"* bölümü — M15-07'nin çakışma fonksiyonunu
   tenant'ın kendi bütün kademelerine uygular ve öneriyi verir; panelin sıradan, RLS'li okumasıdır. Yayından
   önce her pilot tenant'ın owner'ı (bugün KF ve KM) bölümü açar ve kesimini ayarlar. Kesimler **motor yeni
   kuralla karar vermeden önce** ayarlanmalıdır; bunu M15-04'ün tek yapılandırma değeri sağlar
   (`TAPPA_BUSINESS_DAY_FROM`, M15-04 adım 3b): sürüm onsuz çıkar (`T0`; motor kesimsiz — yalnız 18 sa
   kuralı —, ayar ekranı açık), owner'lar kesimi ayarlar, değer yayın anına konur (ConfigMap) ve yeniden
   başlatılır (`T2`). **`T2`, `T0` ile aynı gündür** (ürün merceği P1): aradaki bekleme *"güvenli"* değildir —
   zaten ters olan zincirler kesimsiz kuralla düzelmez (dün 17:40 `in` aslında çıkıştı → bugün 09:00, 15 sa
   20 dk < 18 sa → yine `out`). **Repoya RLS'i atlayan bir `cmd` aracı eklenmez**, yeni bir `op_*` açılmaz.
3a. **Canlıda dört tenant var** (`state.md` OP-15 notu: *"askıda tenant 0/4"*); KF ve KM pilottur, öteki
   ikisi `T2`'de sessizce işletme varsayılanı 05:00'e geçer. **Karar satırı (kullanıcıya, deploy'dan önce):**
   pilot olmayan iki tenant için kesim ne olacak — owner'ları bilgilendirilip kendi kontrolünü mü yapacak, yoksa
   05:00 bildirimle mi kabul edilecek. Her iki durumda: *"Business day check"* bir çakışma bulursa owner
   panelinin **her ekranında** bir `PanelNotice` (*"Your business day setting cuts 2 shifts — review it"*),
   `T0`'dan itibaren, owner bölümü açıp kaydedene ya da çakışma kalkana kadar — RLS'li sıradan bir okuma, yeni
   `op_*` yok (ürün merceği P2).
4. **Yayın günü panel duyurusu** (ürün merceği Ü-O6, P1): anomali ve rapor ekranında, yayından sonraki ilk
   haftalarda tek bir `PanelNotice`, iki cümle iki ayrı ana bağlı:
   - **liste cümlesi `T0`'dan** (sınıflandırma sürümle devreye girer, anahtara bağlı değildir): *"The open
     check-in list now also shows entries an older rule had hidden."*;
   - **iş günü cümlesi yalnız anahtar etkinken**: *"From <T2>, a check-in is closed only within its business
     day; records received before that were paired by the old rule."* — tarih `TAPPA_BUSINESS_DAY_FROM`'dur
     (motorla aynı değer). *"Received"*: anahtar sunucunun `Now`'ıyla karşılaştırılır (F-2), yani ölçüt kaydın
     sunucuya ulaştığı an, beyan ettiği an değil (motor merceği N1).
5. **Deploy notu** (`state.md`, ana oturum): **Faz A ayrı sevk edilir (K6)**; sıra `T0` (sürüm) → owner'lar
   kesimi ayarlar → `T2` (anahtar), **aynı gün**; pilot olmayan iki tenant için 3a'nın kararı; çalışanlar ilk
   gün *"Your previous clock-in has no clock-out"* notunu bir kez görebilir; geri dönüş `Down` değil
   binary'dir, anahtarı kaldırmak yeni dokunuşları kesimsiz kurala döndürür (ADR 0032 §*"Geri alma"*, §A.8).
   **Bu cümle deploy notunda ve bu kartın kullanıcıya verilen raporunda aynen yer alır** (ürün merceği Q1):
   *"Faz A'da çalışanın tap ekranı değişmez ('Tap'); düzelen şey kaydın yönüdür ve sonuç ekranında 'Tapped
   in/out' olarak görünür. 'Clock in/out' düğmesi ve 'Not right?' bağlantısı Faz B ile gelir."*

**Kabul kriterleri.**
- Tam suit yeşil (`-race`) — M15-04'ün saatten bağımsızlık testleri dahil; `make audit` exit 0;
  `TestEveryNamedTestExists` yeşil; güvenlik merceği ONAY.
- KM'nin bir günü (fırın 04:00–12:00 dahil) seed kesimiyle doğru yönlerle geçer.
- *"Business day check"* bölümü owner dışında görünmez (manager 403/gizli — `mayEditAccount`), yalnız kendi
  tenant'ını okur (RLS testi); Q30 cevaplandı; duyurunun render testi; `Down`'ın dolu satırla reddi bir kez
  elle ölçüldü.
- **Faz A ayrı sevk edilir (K6):** bu kartın onayıyla Faz A `main`'e gider (birleştirme anı kullanıcının);
  M15-06 bu kapının parçası değildir (ADR 0032 §A.6: Faz A'da motor çıkışı (b)/(c) olmaz) — M15-14'ün
  kapsamındadır.
- Duyurunun iki cümlesi iki modda doğru (render testi: anahtar yokken yalnız liste cümlesi); çakışma
  `PanelNotice`'i owner panelinin her ekranında, çakışma yokken hiçbirinde; 3a'nın kararı `state.md`'de.

---

## Faz B — Tahmini yön ve düzeltme

## M15-09 — Şema: `direction_source` (00065) ve iki CHECK'in doğrulanması (00066)

- **Bağımlılık:** M15-08
- **Kırmızı çizgi:** §4.3 (`Down` kanıt silmez), §4.6, §6
- **Araç:** agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(db): record who chose the direction of a tap`

**Amaç.**
- **00065:** `SET LOCAL lock_timeout = '1s'`; `direction_source text` + `CHECK (direction_source IN
  ('auto','employee'))` ve `CHECK (direction_source IS NULL OR type IS NOT NULL)`, ikisi `NOT VALID`.
  `InsertTransaction` parametresi; `store.Transaction`'ı tam döndüren sorgular güncellenir. Tarihsel satırlar
  **NULL** (okuyucu kuralı: yönlü nfc/qr satırında NULL = *"M15 öncesi, motorun seçimi"*; tek yardımcı).
  `Down`: dolu bir değer varsa **reddeder**.
- **00066:** 00064'ün ve 00065'in CHECK'lerinin `VALIDATE CONSTRAINT`'i (`00044`'ün deseni ve gerekçesi:
  ayrı dosya, `SHARE UPDATE EXCLUSIVE`, tap'in INSERT'i beklemez). `Down`: aynı metinlerle `NOT VALID` hâli.

**Kabul kriterleri.** Boş veritabanında `Down`'lar geri alır, dolu değerle 00065 reddeder; `'manager'`
reddedilir; yönsüz satıra değer yazılamaz; 00065'in `ALTER`'ı satır taramaz, 00066 tarar (00043/00044'ün
ayrım testi deseni); feed yazıcısının kolon yetkileri genişlemez.

**Tuzaklar.** `DEFAULT 'auto'` · CHECK'i doğrulamalı eklemek · koşulsuz `Down`.

## M15-10 — Tap motoru: istenen yön ve tahmin

- **Bağımlılık:** M15-09
- **Kırmızı çizgi:** §5, §4.6, CLAUDE.md §8
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(tap): honour the direction the employee chose`

**Amaç.** `Input.RequestedDirection *Type`, `Decision.DirectionSource`, dışa açık `PredictDirection` —
`resolveDirection` onu çağırır.

**Kural (ADR 0032 §B.3).** `D` motorun yönü. İstek yoksa ya da `D` ise → `D`, `auto`; farklıysa → istenen,
`employee`. Yalnız `ok`/`flag`'te; verdict, güven puanı etkilenmez; geç kalma kaydedilen yöne göre.
**İki dilli not** (ADR 0032 §B.4): kayıt *"direction chosen by employee"*, `EmployeeNote` *"You chose
Clock out."* (metinler Q33 ile birlikte kesinleşir). **Açık bırakılan giriş:** çalışan `in` seçip aynı iş
günündeki açık girişi açık bırakırsa (bölünmüş vardiya — ürün merceği Ü-O2) `Decision` onu **çıkış talebi
çapası** olarak işaretler (neden: *çalışan seçimi*) — Faz C kartı bu nedende de **açılır** (önceki sürümün
*"bu nedende açılmaz"* kararı değişti). Çalışan `out` seçip çıkış yetimse (b/c) karar *"giriş eksik"*
çapasıdır.

**Kabul kriterleri.** (`TestDecide_` önekli)
- Dört hücre (tahmin × istek); `flag`'te istek uygulanır; `reject`/`ignored`'da yön ve kaynak yok.
- `PolicyContext` anahtar kümesi değişmez; `PredictDirection` ile `resolveDirection` aynı yönü verir
  (özellik testi).
- Bölünmüş vardiya: 11:00 `in`, 18:00 `want=in` → kayıt `in`, `employee`; 11:00 girişi *çalışan seçimi*
  nedeniyle çıkış talebi çapası.
- `EmployeeNote` ve `Note` ayrı metinler; `partnerKeptAnnotations` yeni kayıt notunu tanır.
- CLAUDE.md §5 ikinci paragraf (ADR 0032) bu commit'te.

**Tuzaklar.** İsteği verdict'e bağlamak · `employee`'yi *"istek geldi"* diye yazmak.

## M15-11 — `POST /api/checkin`: istenen yönün yolu

- **Bağımlılık:** M15-06, M15-10
- **Kırmızı çizgi:** §4.6, §4.5, ADR 0006, §7
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(checkin): carry the employee's chosen direction to the decision`

**Amaç.** Form alanı `want` → kilitli bölümde `RequestedDirection` → satırda `direction_source`. **Hiçbir
`want` biçimi 400 değildir** (ADR 0032 §B.3).

**Adımlar.** saf yorum fonksiyonu (tablo testli) · `checkin.Request` · `insertParams` (`direction_source`
yönsüzde ve manuelde NULL) · `Result`'a kaynak · `partnerKeptAnnotations` · `devtap.go` · M9-01 kartına
not (kuyruk `want`'ı saklar).

**Kabul kriterleri.**
- DB testi: tahmin `in`, `want=out` → `out`, `employee`, `ok`; `want` yok → `auto`.
- **JS'siz POST (gerçek router):** tek `want`; `want=` → `auto`; `want=out&want=out` → `out`;
  `want=in&want=out` ve `want=sideways` → motorun yönü, `auto`, uyarı logu (değer loglanmaz) — her biri tam
  bir satır, hiçbiri 400.
- Eşzamanlılık (`-race`): `want=out` + `want` yok iki POST → biri `ignored`, `direction_source` `D` ile
  tutarlı. Bayat sayfa (sınır geçti) + ana düğme → `out`, `employee`, M15-06'ya göre yetim.
- Eş mekân: işverende yazılır, `TestEvidenceReaders_EveryOneIsClassified` yeşil.
- **`want` yalnız anahtar etkinken uygulanır** (güvenlik merceği F-1, ADR 0032 §B.5): anahtar yokken ya da
  sunucunun `Now`'ı değerden önceyken gelen `want` **yok sayılır** — kayıt motorun yönüyle, `auto` (S yokken
  çalışanın seçtiği önsüz bir çıkış (d) olur ve hiçbir yerde görünmezdi). Negatif test: iki kapalı modda
  `want=out` → motorun yönü, `auto`, tam bir satır. Kapı M15-04'ün **tek** fonksiyonudur.
- **Sevk kapısı:** (1) Q31(a) cevaplandı ve M15-13 gerekiyorsa bitti; (2) **anahtar canlıda ayarlı ve
  geçmişte** (K6 ile Faz A önce çıktığı için normalde zaten öyle) — ikisi olmadan bu kart `main`'e girmez.

## M15-12 — `GET /t` tahmini ve tap ekranı (§9)

- **Bağımlılık:** M15-11
- **Kırmızı çizgi:** §9, §4.2, §4.4 (sayaç okunmaz), §4.6, §4.5
- **Araç:** yapıcı, skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(tap): say the predicted direction on the tap button`

**Amaç.** `GET /t` tahmini (`@at = now` + etkin kesim + bölge → `tap.PredictDirection`) ve tap ekranı:
- Birincil düğme *"Clock in"* / *"Clock out"* — `<button type="submit" name="want" value="…"
  class="tap-button" data-tap-button>`; accent slotu bu düğme.
- Altında *"Not right? Clock out"* — `<button type="submit" name="want" value="…">`, ink, altı çizili,
  `min-h-11`, accent yok; **ana düğmeyle arasında en az 24 px dikey boşluk** (ürün merceği D2).
- HTML'de **statik gizli `want` yok**.
- **Tahmin kapısı** (güvenlik merceği G-O3 ve F-1, motor merceği ORTA-2, K5; ADR 0032 §B.2): tahmin ve
  bağlantı yalnız **anahtar etkin** (M15-04'ün **tek** kapı fonksiyonu — karar ile tahmin aynı fonksiyonu
  çağırır) **ve** (NFC'de `pv.CMACValid` | QR'da **`p.Channel == qr` ve `PreviewWithoutReplayProtection`
  hatasız** — QR'da CMAC yoktur, `internal/sun/preview.go:93-95`, `:232-235`) **ve** `pv.TagStatus == active`
  **ve** çalışan `active` iken; aksi hâlde
  bugünkü yönsüz **"Tap"** düğmesi, `name`/`value` yok, bağlantı yok. Tahmin okuması başarısızsa da aynı.
  Merceğin önerdiği salt-okunur `ctr > last_ctr` şartı **eklenmez**: `sun.Preview` bilerek sayaç taşımaz
  (`internal/sun/preview.go:57-65`; yansıma testleri bir sayaç alanında kırmızı) — bunu geri getirmek §4.4'ün
  TOCTOU malzemesini sayfaya verir (güvenlik merceği bu reddi DÜŞÜK kabul edilmiş risk olarak kabul etti; sunucu
  içi bir *"URL harcanmamış"* boolean'ı ADR 0032 §B.2'de bağlayıcı olmayan gelecek seçenek).
- **QR'da tahmin var (K5, 2026-10-21 — 5. sürümün *"QR'da tahmin yok"* kararının yerine).** QR URL'si imzasızdır (`?tag=<uid>` — `internal/sun/params.go:145-146`, `:185-188`): bilinen bir uid ve çalınmış bir oturum çerezi yeter; eski bir NFC URL'sinden `ctr` ve `cmac` silinince QR olur, bu yüzden NFC'deki CMAC şartı tahmin için bir fren **değildir** — kapının fiilî hâli *anahtar etkin + plaket aktif + çalışan aktif*'tir. Risk kullanıcının K5 gerekçesiyle aynıdır: açığa çıkan yalnız oturum sahibinin kendi durumu. Kabul edilmiş
  sınırlar: biriktirilmiş NFC URL'si ve fotoğraflanabilir, süresiz statik QR tahmini oturumun sahibine kendi
  durumu olarak gösterir; fotoğraflanmış QR + **çalınmış oturum çerezi** ile bir başkası *"mesaide mi"*
  sorgulayabilir — kabul edilmiş risk (güvenlik merceği v4 O-3; ADR 0032 §B.2). QR kaydı IP yoksa zaten `flag`.

**Adımlar.**
1. `tenant.Directory.TapPage` / `PeerTapPage` → `TapPageFacts`'e tahmin (eş mekânda işverenin zinciri);
   etkin kesim `tap` paketindeki **tek kapı fonksiyonundan** (M15-04 adım 3b; ayrı bir kesim okuması ya da
   ayrı bir anahtar kontrolü yazılmaz — motor merceği ORTA-2, N4); anahtarın değeri `tenant.Directory`'ye yapı
   alanı olarak gelir, `checkin`'i içe aktarmaz; plaket/çalışan/kanal şartları handler'da `pv` ve `p` ile.
2. `pages.TapView`, `tap.templ` (`tapForm`, `TapButtonFace`); `tap.templ:12-17` ve `:52-53`'ün yorumları
   (*"DIRECTION-NEUTRAL"*, *"its word 'Tap'"*) ADR 0032'ye atıfla yeniden yazılır.
3. **`tap.js`:** `submit` işleyicisinin ilk işi — `markPending`'den ve konum dalından önce — basılan
   düğmenin değeri (`event.submitter`; yoksa `click` işleyicisinin sakladığı) **tek** gizli `want` alanına
   (varsa yeniden kullanılır ve güncellenir; bfcache ya da ikinci gönderim iki alan üretmez); sonra iki
   düğme birlikte devre dışı. GPS çağrısı tek.
4. Account önizlemesi (`TapButtonFace(TapButtonPreview)`): *"Clock in"* yüzü, bağlantı yok, `type="button"`.
5. Goldens: tap render'ları bilinçli yeniden üretilir (diff satır satır raporda); sonuç ve aktivasyon
   golden'ları değişmez.
6. **Güncellenecek metinler** (ürün merceği D4): CLAUDE.md §9 tap paragrafı; skill `tappa-brand`
   `SKILL.md:484` (*"kelimesi 'Tap' … tek yeni metin logonun alt'ıdır"*) ve *"Tap ekranı — kutsal alan"*;
   ADR 0023 §6 madde 10'a (`0023:861`) tarihli not.

**Kabul kriterleri.**
- Üç şekil render testiyle (tahmin `in`, `out`, yok); hiçbirinde statik gizli `want`.
- **Pozitif render testi (K5):** geçerli QR bağlamı, aktif plaket, aktif çalışan, açık girişi var → *"Clock
  out"* + *"Not right? Clock in"*; NFC'de aynı.
- **Negatif render testleri (G-O3, F-1):** CMAC doğrulanmayan bir NFC URL'si; `retired`/`lost`/`unassigned`
  plaket — **NFC'de ve QR'da**; deaktive çalışan — **NFC'de ve QR'da**; **anahtar yokken** ve **sunucunun
  `Now`'ı anahtardan önceyken** (iki mod) → yönsüz *"Tap"*, `want` yok, bağlantı yok.
- **Tek kapı:** tahminin ve kararın aynı kapı fonksiyonunu çağırdığını iddia eden bir test (kapıyı tek yerde
  kapatan mutasyon iki yüzeyi birden yönsüz bırakır).
- Tek `tap-button`; bağlantı accent taşımaz (`TestTapPage_TheBrandFillsTheTwoSlotsAndNothingElse` genişler).
- **`tap.js` kaynak düzeyi test** (repoda JS çalışma zamanı yok — `internal/handler/tap_test.go:719-725`'in
  sınırı; `TestTapScript_DoublePressCannotEscapeAsANativeSubmit`'in deseni): `event.submitter` (+ `click`
  yedeği) okunuyor; gizli alan önce aranıyor, yoksa oluşturuluyor; yazım `markPending`'den, konum dalından ve
  `form.submit()`'ten önce; iki düğme devre dışı. Davranış M15-14'te tarayıcıda.
- `getCurrentPosition` tek; `GET /t` konum okumaz; eş mekânda tahmin işverenin.
- 390 / 834 / 1200 px'te yatay kaydırma yok; **390 px ekran görüntüsünde bağlantı ile 64 px düğme arası
  ≥ 24 px** ölçülür; ikinci hedef ≥ 44 px; kontrast AA.

**Tuzaklar.** Bağlantıyı `<a href>` yapmak · ikinci `<form>` · statik gizli alan · tahmini kapısız
hesaplamak · sayacı sayfaya geri getirmek.

## M15-13 — Koşullu: `/v1` punch akışına `directionSource` (00068)

- **Bağımlılık:** M15-11 · **Koşul:** Q31(a) *evet* (canlıda aktif API anahtarı ya da webhook'u olan bir
  tenant varsa öneri *evet*) — *hayır* ise kart **atlanır** ve gerekçesi ledger'a yazılır
- **Kırmızı çizgi:** §4.5 (feed yazıcısı tanımlayıcı rolü), §4.3, ADR 0028 (sözleşme)
- **Araç:** yapıcı + agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(api): say who chose a punch's direction`

**Amaç.** Güvenlik merceği G-O6: `/v1` punch akışı canlı (`00048`–`00052`), olayın `direction`'ı
`transactions.type` (K-40); çalışanın seçtiği yön dış sisteme işaretsiz gider ve dış sistemin kendi eşlemesi
ADR 0032 §A.6'nın korumasını taşımaz. `punch_events`'e eklemeli, isteğe bağlı `direction_source` (snapshot,
yazımda bir kez) + feed yazıcısının kolon yetkisi + OpenAPI + entegrasyon rehberi (ADR 0030) +
`FeedCatalog` testi aynı commit'te. `Down` dolu değerde reddeder.

**Kabul kriterleri.** v1 istemcisi alanı bilmeden çalışır (geriye uyum testi); işveren ve saha satırında
aynı değer; manuel satırlar akışa yine girmez (K-6); M11'in feed testleri yeşil. **Saha tenant'ına geçen
sütun:** alan holding punch'ının saha satırına da yazılır; ADR 0027 §9 madde 3 (`0027:915-918`) sahanın
gördüğü sütunları katalogla pinler — o maddeye **tarihli bir K notu** (*"directionSource: sahanın gördüğü
sütunlara eklendi, M15-13"*) ve katalog pininin bu sütunla genişletilmesi (pin gevşetilmez, yeni sütunu
adıyla içerir) bu kartın kabul kriteridir.

## M15-14 — Faz B kapanışı

- **Bağımlılık:** M15-06, M15-09 … M15-13 · **Araç:** ayrı denetçi + agent `tappa-security-auditor` · **Mercek: iki**
- **Kırmızı çizgi:** hepsi — özellikle §4.2 (`tap.js`), §4.6 (`want`), bordro

**Kapsam:** Faz B kartları **ve M15-06** (yetim çıkış — K6 ile Faz A ayrı çıktığı ve M15-06 Faz A'nın
kapısının dışında kaldığı için hiçbir faz kapanışının güvenlik merceği onu görmüyordu; M15-11 ona bağlı —
motor merceği DÜŞÜK-3).

**Adımlar.** Tam `make check`, `make audit`; güvenlik merceği Faz B diff'inde **ve M15-06'nın diff'inde**;
ekran görüntüsü turu.
**Gerçek tarayıcıda elle doğrulama:** ağ panelinde `POST /api/checkin` gövdesi — JS açık + konum izni var
(ana düğme, bağlantı), JS açık + konum API'si yok/reddedildi, JS kapalı, **bfcache'ten geri dönüp ikinci
basış**.

**Kabul kriterleri.** Tam suit yeşil; `make audit` exit 0; `TestEveryNamedTestExists` yeşil; güvenlik
merceği ONAY (M15-06 dahil); beş elle vakada gövdede **tam bir** `want` ve basılan değer, her vakada tam bir
satır; Q31 cevaplandı ve M15-13 gerekiyorsa bitti; **anahtar canlıda ayarlı ve geçmişte** (`state.md`'de
değeri ve ayarlandığı an).

---

## Faz C — Eksik yarı talebi

## M15-15 — Şema: `punch_claims` ve `punch_claim_decisions` (00067)

- **Bağımlılık:** M15-14
- **Kırmızı çizgi:** §4.5, §6, §4.3'ün kardeşi (append-only), §4.7
- **Araç:** agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(db): add punch claims and their decisions`

**Amaç.** ADR 0032 §C.2'nin iki tablosu: `punch_claims` (`anchor_transaction_id`, `kind`, `claimed_at`,
**`prefilled_at`** — gerçek vardiya ucu, yalnız pencerenin içindeyse; vardiyasız çalışanda ya da uç pencere dışındaysa
NULL; *"varsayılan değiştirilmedi"* bir hesaptır: `prefilled_at IS NOT NULL AND claimed_at = prefilled_at`) ve
`punch_claim_decisions` (`outcome`, `decided_by`, `approved_at`, `transaction_id`, `note`). İkisi de:
ENABLE + FORCE RLS, `NULLIF` politikası, `tenant_id` önde indeks, `GRANT SELECT, INSERT`, `REVOKE UPDATE,
DELETE`, `tappa_forbid_mutation` satır tetikleyicisi **ve** `*_no_truncate` deyim tetikleyicisi
(`00021:96-116`'nın deseni — güvenlik merceği G-O2). Talep INSERT'i çapa satırından kurulur. `Down` satır
varsa reddeder.

**Kabul kriterleri.**
- RLS izolasyon testi (filtresiz sorgu); UPDATE/DELETE `tappa_app`'ta yetki hatası, `tappa_owner`'da
  tetikleyici hatası.
- **TRUNCATE:** iki tablo `internal/db/appendonly_truncate_test.go:88-95`'in `truncateTargets`'ına eklenir;
  `tappa_owner`'ın `TRUNCATE`'i tetikleyiciyle reddedilir.
- İkinci talep aynı çapaya → unique ihlali; ikinci karar → unique ihlali; yabancı çapa → 0 satır.
- `redline-check.sh` R5 iki tabloyu tenant tablosu sayar; `pg-restore-verify.sh` ENABLE/FORCE eşit.

## M15-16 — Domain: talep penceresi, gönderim, karar

- **Bağımlılık:** M15-15
- **Kırmızı çizgi:** §4.3, §4.6, §4.5, ADR 0006, ADR 0009, ADR 0011, ADR 0025, §7
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(claim): let an employee claim the missing half and a manager decide it`

**Amaç.** Yeni paket `internal/domain/claim` (CLAUDE.md §3 dizin haritasına satır — bu commit'te):

1. **Çapa durumu** — §A.6'nın sınıflandırmasıyla: çıkış talebinin çapası açık bir `in`; giriş talebinin çapası
   yetim (b/c) bir `out`; **çapanın `business_day_start`'ı dolu** (M15 öncesi çapa için talep yok — Ü-Y2).
2. **Pencere** (ADR 0032 §C.3; güvenlik G-O4, motor M-O1):
   - çıkış: `claimed ∈ (in.At, min(next, Next(in.S), in.At + StaleOpenIn, now))` — `next` hariç,
     `Next(in.S)` hariç (sınır anı yeni gün), `in.At + 18 sa` dahil, `now` dahil;
   - giriş: `claimed ∈ (max(prev, out.S, out.At − StaleOpenIn), out.At)` — `prev` hariç, `out.S` dahil,
     `out.At − 18 sa` dahil, `out.At` hariç.
3. **Ön dolgu (K3; ürün + güvenlik mercekleri):** çapanın çözülmüş vardiyasının **gerçek** ucu — bitiş (çıkış)
   ya da başlangıç (giriş) — **yalnız pencerenin içindeyse** `prefilled_at` olur. Uç pencerenin dışındaysa ya
   da vardiya yoksa `prefilled_at` NULL, alan **boş ve zorunlu**. Pencereye **kırpılmış** bir ön dolgu yoktur:
   KF 10–22, 11:00 `in`, 15:00 unutuldu, 18:00 *"Not right? Clock in"* → pencere `(11:00, 18:00)`; 22:00'yi
   17:59'a kırpmak *"default not changed"* + bant içi → tek tıkla ~7 sa (gerçek 4 sa). *"Default not changed"*
   yalnız `prefilled_at` doluyken anlamlıdır. Açık uca kırpmanın tanımsızlığı da böylece düşer.
4. **Ne zaman sorulur (K4) — tür başına**, durum tutmadan, zincirden (ADR 0032 §C.1):
   - **çıkış talebi** (çapa açık `in` A): (1) A'yı açık bırakan dokunuş `T1` (A'dan sonraki ilk yönlü olay);
     (2) `T1`'in iş gününde `T1`'den sonraki ilk `out`;
   - **giriş talebi** (çapa yetim `out` X): (1) X'in kendi sonuç ekranı; (2) X'in iş gününde X'ten sonraki
     ilk `out`.
   Saf fonksiyon: `(çapa, tür, kişinin çapadan sonraki yönlü olayları, bu dokunuş) → göster | gösterme`.
   Kart yalnız sonucu `ok` ya da `flag` olan dokunuşta çıkar; `ignored`/`reject` yönsüzdür, kart çıkmaz ve
   fırsat sayılmaz. **Yarış (ürün N3):** aynı dokunuşta iki çıkış çapası (dünden açık A, bugün çalışanın seçimiyle
   açık bırakılan B) → en taze çapa B gösterilir, A anomali listesinde. **M9-01 notu:** çevrimdışı kuyruktan
   sonradan gelen geçmiş bir satır *"çapadan sonraki ilk olay"*ı değiştirip hangi iki dokunuşun fırsat olduğunu
   kaydırabilir; M9-01 kartı bunu bilir (sınır yine iki fırsattır).
5. **Gönderim** (kilit altında): çapa hâlâ eksik, çalışan `active`, an pencerede → INSERT + `audit_log`
   (`claim.submitted`, `ActorID` nil — çalışan kendisi adına, `internal/audit/audit.go:59-61`). Aynı çapaya
   ikinci gönderim unique ihlali (23505) → `ErrAlreadySent` (handler *"Sent"* ekranına eşler).
6. **Bant (K3, G-Y1):** talep edilen an vardiyanın §A.7 bandının dışındaysa ya da vardiya yoksa karar
   **onay adımı ister** (`RequiresConfirmation`); sapma (*"4 h before the shift starts"*), oluşacak aralık ve
   süresi domain'de hesaplanır (kuyruk yalnız gösterir). **Müdürün *Edit* saati de aynı banda tabidir** (ürün
   N8 — bilerek): bant dışı bir *Edit* aynı onay adımını ister; iki yolun ayrı kurala tabi olması *"Edit'le
   gir, adımı atla"* kısayolunu açardı.
7. **Karar** (kilit altında): pencere ve çapa durumu yeniden doğrulanır; **onaylanacak manuel satırın zincire
   eklendiği hâli `tap`'in sınıflandırmasıyla sınanır — yeni çift gerçekten `closes` olmalı**; onay (ya da
   *Edit*) → `manual.Recorder`'ın işlem alan varyantı (mekân = çapanın mekânı; eş mekânda
   `InsertManualSiteTransaction` yolu; eşlik bittiyse `ErrVenue`), karar satırı, `audit_log` — **tek işlem**.
   Ret → karar + audit.
8. **Öz-onay** (G-O1): `decided_by` ile `employee_id` karşılaştırması yazılır ama **bugün erişilemez** (iki
   kimlik uzayı — `internal/handler/review.go:470-473`); kod ve ADR bunu güvence diye sunmaz; M14'e bağlı.
9. **Süre, gereksizlik, deaktivasyon** (ADR 0032 §C.5); **askı:** karar yolu ana yazma grubunda, gönderim
   tap yüzeyinde (ADR 0032 §C.4).

**Kabul kriterleri.**
- **Pencere tablo testi + mutasyon** (G-DÜŞÜK): her uç için bir içeride/dışarıda çifti; her ucu bir birim
  kaydıran ya da hariç↔dahil çeviren mutasyon kırmızı. Rusty Bar çıkışı 02:00'ye kadar; `Next(in.S)` tam
  anında ret; `in.At + 18 sa` kabul, + 1 sn ret.
- **G-Y1 vakaları:** 09:00 seçilmiş `out` (c), giriş talebi 05:00 → pencere içinde ama **bant dışı** →
  `RequiresConfirmation`; çıkış talebi Sal 03:00 (17 sa 48 dk) → 18 sa içinde, bant dışı → onay adımı.
- Üçüncü göz vakası (Pzt 15:00 `in`, Sal 07:00 seçilmiş `out`) → iki talep de kabul; onaydan sonra Pzt 8 sa.
- **Ön dolgu (iki kriter):** (1) KF 10–22 bölünmüş vardiya vakası → vardiya bitişi 22:00 pencerenin
  `(11:00, 18:00)` dışında → `prefilled_at` NULL, alan boş ve zorunlu, *"default not changed"* yok;
  (2) Pzt 09:12 `in`, vardiya 09:00–17:00, ertesi gün çıkış talebi → 17:00 pencerede → `prefilled_at` = 17:00,
  değiştirilmeden gönderilirse *"default not changed"* ve bant içi.
- **K4 — çıkış talebi:** A açık, `T1` (in), `T2` (in, aynı gün), `T3` (out, aynı gün), `T4` (out) → kart `T1`
  ve `T3`'te *göster*, `T2` ve `T4`'te *gösterme*; talep gönderildiyse ikinci gösterim yok.
- **K4 — giriş talebi:** X (yetim out), `Y` (in), `Z` (out, X'in günü) → kart X'te ve Z'de *göster*, Y'de
  *gösterme* (3. sürümün formülü burada üç gösterim veriyordu).
- **K4 — yönsüz sonuç:** `ignored` ya da `reject` sonuçlu bir dokunuşta kart yok ve sayılmaz.
- **Yarış (N3):** aynı dokunuşta A ve B çıkış çapası → B gösterilir.
- *Edit* saati bant dışı → onay adımı.
- Yeniden doğrulama: pencere aritmetiğini bozan bir mutasyon sınıflandırma sınamasıyla yine kırmızı.
- ADR 0011 ölçümüyle hiçbir onay bir aralığı uzatmaz; aynı talebe iki eşzamanlı onay → tam 1 karar, 1 satır;
  yarıda kalan onay → hiçbir satır.
- Log ve `audit_log` sır/e-posta taşımaz; CLAUDE.md §3 satırı bu commit'te.

## M15-17 — Sonuç ekranı talep kartı ve `POST /api/claim` (§9 istisnası)

- **Bağımlılık:** M15-16
- **Kırmızı çizgi:** §9, §4.5, §4.6, §4.2, §7
- **Araç:** yapıcı, skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(tap): ask for the missing half on the result screen`

**Amaç.** Kayıt yazıldıktan sonra sonuç ekranında isteğe bağlı tek kart (ADR 0032 §C.1): çıkış eksik
(*"Yesterday you clocked in at 09:12 but there's no clock-out. What time did you leave?"* — ya da aynı gün
için *"You clocked in at 11:00 and didn't clock out. What time did you leave?"*), giriş eksik (*"What time
did you come in?"*). Koşullar: M15-16'nın çapa durumu (S dolu), talep yok, `manual.MaxBackdate` içinde, K4
*göster*. Saat alanı `prefilled_at` ile dolu ya da (vardiyasız ya da vardiya ucu pencere dışı) boş ve
zorunlu. *"Send to my manager"*;
atlama düğmesi yok.

**Okumanın yeri** (güvenlik merceği G-O5): kartın okuması kayıt işleminin **içinde değildir** (`gatherPerson`
içinde bir hata kaydı düşürürdü — `checkin.go:1388-1390`, iptal edilmiş işlem 25P02) ve isteğin bağlamında
**da değildir** (WL-9 S1: 200 + boş gövde — `handler/checkin.go:250-268`). Commit'ten sonra, `post` bağlamında
(isteğin değerleri, iptali yok), `resultBrandWait` gibi **sınırlı** bir sürede okunur; hata ya da zaman aşımı
→ kart çıkmaz, sonuç ekranı aynen çizilir.

**`POST /api/claim`:** `Tap.Mount` grubuna bağlanır (`internal/handler/tap.go:270-276`, `chain()` —
ByAddress → Identify → BySession), aynı Origin kuralı, `SessionLive`, imzalı talep bağlamı (yeni MAC etiketi,
oturum id'si ek veri, 15 dk), duvar saati → pencere içindeki aday anlardan **erken** olanı (sonbahar),
`claim.Submit`. Yanıt *"Sent — your manager will review it."*. **Askı:** tap yüzeyi, askıyı bilmez.

**CLAUDE.md ve skill (bu commit'te, tek yerde):** §5 *"Unutulan çıkış"* cümlesi; §9 onay ekranı istisnası;
skill `tappa-brand`'in sonuç ekranı maddesi.

**Kabul kriterleri.**
- Kartsız sonuç ekranları **bayt-aynı** (`TestUnbrandedScreens_AreByteIdenticalToTheGolden`,
  `TestResultScreen_DrawsTheLogoAndNoAccent` değişmeden yeşil); kartlı şekiller için yeni golden'lar;
  accent yok.
- **Ü-Y2:** S'si NULL (M15 öncesi) bir açık girişin ardından gelen dokunuşta kart **çıkmaz**.
- **Hata enjeksiyonlu handler testi:** kartın okuması hata ve zaman aşımı verince 200, tam gövde, kart yok,
  bir kayıt.
- **Çapraz etiket negatif testi:** talep bağlamı `/api/checkin`'de, tap bağlamı `/api/claim`'de reddedilir;
  başka oturumun bağlamı 400, satır yok.
- **Çift *"Send"*** → ikincisi *"Sent"* ekranı (500 değil), bir satır.
- Süresi dolmuş bağlam → *"this question has expired"*; pencere dışı saat → form yeniden.
- K4: aynı çapa için kart tam iki dokunuşta; `ignored`/`reject` sonucunda kart yok.
- **`flag` sonucu (ürün N5):** kart `ok` ve `flag`'te çıkar; `flag`'te sonuç ekranının kapanış cümlesi
  (*"…waiting for your manager"*) kartın **altında** kalır — render testi iki şekil için.
- **Not bastırma (ürün N6):** kart varken `EmployeeNote`'un *"Your previous clock-in has no clock-out."*
  cümlesi **gösterilmez** (kart aynı şeyi soruyor); kaydın notu değişmez. Kart yoksa (atlanmış, iki fırsat
  dolmuş, S NULL) cümle gösterilir.
- `getCurrentPosition` sonuç ekranında yok; CLAUDE.md §5/§9 ve skill bu commit'in diff'inde.

## M15-18 — Panel: talep kuyruğu

- **Bağımlılık:** M15-16
- **Kırmızı çizgi:** §4.5, §4.3, §9 (panel), ADR 0025
- **Araç:** yapıcı, skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(panel): decide a punch claim from the review queue`

**Amaç.** Onay kuyruğunun **odak desenini** izleyen ikinci bölüm *"Time claims"* (ürün merceği Ü-O4;
`web/templates/pages/review.templ:12-24`: kuyruk bağlantı listesi, odaktaki tek kayıt için tek form, iki
sonuç iki gönderim düğmesi, betik yok, kısayol yok). *"Tek tık"* = odak + **Approve**; *Edit* odak formunun
içinde bir `<details>` (saat alanı). **Toplu onay yoktur** (`internal/handler/review.go:49-57`'nin kararı,
aynı gerekçe).

**Odaktaki talep müdüre gösterir (K3, G-Y1):** çapa (an, yön, `direction_source = employee` ise işaretli,
kanal) · komşu olaylar · **oluşacak aralık ve süresi** · çözülmüş vardiya ve sapma · *"default not changed"* (yalnız `prefilled_at` doluyken)
işareti. **Bant dışı ya da vardiyasız** talepte Approve doğrudan yazmaz: odak formu aralığı ve sapmayı
yineleyen bir onay adımı ister.

**Rota:** karar POST'ları `mountWriting`'in ana grubu (`ProtectWriting()`, `dashboard.go:166`, flag kararı
`:167`'nin yanı) — askıda 403. Rol: owner ve manager — bugünkü flag kuyruğuyla aynı (ölçüldü: `review.go:359`
`reviewDecision`'da rol kontrolü yok). Anomali ekranında *"çıkış eksik"* / *"giriş eksik"* satırlarında
*"claim pending"* rozeti.

**Kabul kriterleri.** Odak + Approve tek POST (PRG); bant dışı talepte ilk POST yazmaz, onay adımı yazar;
ikinci karar *"already decided"*; başka tenant'ın id'si → bulunamadı; süresi dolmuş/*already resolved*
talepte yalnız Reject; ekranda toplu seçim/düğme yok (render testi); askıda karar POST'u 403; G-Y1'in iki
vakası kuyrukta aralık + süre + sapma + işaretle görünür; M12 `Panel*`; 44 px.

## M15-19 — Son kapanış

- **Bağımlılık:** hepsi · **Araç:** ayrı denetçi + agent `tappa-security-auditor` · **Mercek: iki**
- **Kırmızı çizgi:** hepsi — özellikle §4.5 (yeni tablolar, yeni POST'lar), §4.3, §4.7
- **Commit:** `docs(adr): record the business day and the direction chain as built` (+ düzeltmeler)

**Adımlar.** Tam `make check`, `make audit`; güvenlik merceği bütün M15 diff'inde; ADR 0032'ye *"Uygulandı"*
notu; ekran görüntüsü turu; **toplu geçiş deploy notu** (`state.md`): migration sırası 00064 → 00067
(+ 00068), çakışma raporunun sonucu, panel duyurusunun tarihi, geri dönüşün binary olduğu.

**Kabul kriterleri.** Tam suit yeşil (iki sabit saatli koşum dahil); `make audit` exit 0;
`TestEveryNamedTestExists` yeşil; güvenlik merceği ONAY; RLS ve TRUNCATE testleri iki yeni tabloda yeşil;
uçtan uca DB senaryosu (Pzt unutulan çıkış → Sal sabah `in` → kart → talep → onay → Pzt 8 sa, liste boş;
reddedilince Pzt listede); skill bu kartta değişmedi; `main`'e birleştirme kullanıcının kararı.

---

## Bilinçli olarak yok (M15 kapsamı dışı)

- **Otomatik çıkış üretimi** (Q18).
- **İki yönlü geçmiş gün sorusu** (K2) — tek okutmanın aslında çıkış olduğu gün anomali ekranında.
- **Talep sonucunun çalışana bildirimi** (Q32).
- **Rapor sayacı ve CSV sütunu olarak *"chosen by employee"*** (önceki M15-13, ertelendi) — seçim fişin
  notunda görünür.
- **Bayat sayfa ile bilerek düzeltmenin ayrımı** (imzalı tahmin).
- **Tahmin için sayaç kontrolü** (§4.4). *(QR'da tahmin artık **var** — K5.)*
- **Lokasyon düzeyinde saat dilimi** (Q01).
- **Öz-onay güvencesi** — M14'e bağlı.
- **"On the floor" / şerit okuma ufku** 18 sa kalır.
- **Geçmiş ters kayıtların düzeltilmesi** (§4.3).
