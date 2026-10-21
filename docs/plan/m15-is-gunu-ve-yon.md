# M15 — İş günü ve yön zinciri: gün dönümü, tahmini yön + düzeltme, eksik yarı talebi

> **Durum:** plan (2026-10-21, 2. sürüm — üçüncü göz bulguları işlendi). Kod yok. Canlı durum yalnız
> [state.md](state.md)'dedir; bu dosyaya "yapıldı" yazılmaz.
>
> **Karar belgesi:** [ADR 0032](../adr/0032-is-gunu-ve-yon-zinciri.md) (durum *Önerildi*; 0031
> M14'ündür — aşağıda *"Migration ve ADR numaraları"*). Kartlar ADR'nin sözlüğünü kullanır; ADR kabul
> edilmeden (M15-01) kod kartı başlamaz.
>
> **Atıf kuralı:** kırmızı çizgiler hep **"CLAUDE.md §4.x"**; `file:line` atıfları HEAD `0e7c158`'e
> göredir ve yapıcı kendi turunda yeniden okur.
>
> **Test adı kuralı:** `TestEveryNamedTestExists` reponun bütün metin dosyalarında geçen her `Test…`
> adının var olmasını ister (alt çizgiyle biten önekler, o önekle başlayan bir test varsa geçer). Bu
> belge yalnız **var olan** testleri ve `TestDecide_` gibi var olan önekleri adlandırır; yeni testler
> **tarif edilir**, adlarını yapıcı koyar. Bir kart bir testi yeniden adlandırırsa ona atıf yapan
> belgeyi (bu dosya ve ADR 0032 dahil) **aynı commit'te** günceller.

## Sorun — ölçülmüş kök neden

Kullanıcı (2026-10-21): *"okutmalarda in/out olmadığı için saçma çalışıyor, girerken out
yapabiliyor."*

`internal/domain/tap/decide.go:533-544` `resolveDirection`: kişinin son **açık** girişi varsa bu
dokunuş `out`'tur — **pencere yok**. `StaleOpenIn` (18 sa, `decide.go:481`) yalnız not düşer
(*"stale open check-in (possible forgotten checkout)"*), yön yine `out`. Sorgu
`db/queries/transactions.sql` `GetLastOpenTransaction` da tarihsizdir (*"en son `in`, sonrasında
`out` yok"*). Sonuç: **bir kez** unutulan çıkış zinciri **kalıcı olarak** ters çevirir:

| Gün | Gerçek | Kaydedilen | Neden |
|---|---|---|---|
| Pzt 09:12 | giriş | `in` | açık giriş yok |
| Pzt 17:30 | çıkış (dokunmadı) | — | unutuldu |
| Sal 09:05 | giriş | **`out`** + stale notu | Pzt girişi hâlâ açık |
| Sal 17:40 | çıkış | **`in`** | açık giriş yok |
| Çar 09:00 | giriş | **`out`** | Sal 17:40 `in`'i açık |
| … | … | sonsuza kadar ters | — |

Rapor (`internal/domain/ledger` `accumulate`) kaydedilmiş yönü sırayla eşlediği için ters zincir
**gece aralıklarını** sayar (Sal 17:40 → Çar 09:00 = 15 sa 20 dk < 18 sa `closingTail` → ödenir).

## Kullanıcı kararları (2026-10-21) — bağlayıcı

Yeniden tartışılmaz; ayrıntı ve gerekçe ADR 0032'de.

- **Faz A — iş günü / gün dönümü saati.** Kademe tenant → departman → çalışan, en özel kazanır,
  varsayılan **05:00 yerel**. Açık giriş yalnız **aynı iş günündeyse ve `StaleOpenIn`'den yeniyse**
  kapatılır; değilse bu dokunuş `in`'dir, eski giriş *"çıkış eksik"* anomalisi kalır. Yön politika
  değil **hesaptır** (§5) — policy motoruna girmez. `tap.Decide` saf kalır. Ayar ekranı kesim saati
  bir vardiyanın içine düşerse kaydetmeden önce uyarır. Geçmiş ters kayıtlar düzelmez (CLAUDE.md
  §4.3).
- **Faz B — tap ekranında tahmini yön + düzeltme** (kullanıcının CLAUDE.md §9 onayı, 2026-10-21).
  Tek birincil düğme tahmini yönü söyler (*"Clock in"* / *"Clock out"*); altında küçük metin
  bağlantısı *"Not right? Clock out"* (ya da *in*). Çalışan yönü değiştirirse verdict değişmez
  (`ok` kalır, `flag`'e düşürülmez), nota *"direction chosen by employee"* düşer, raporda görünür.
  `transactions.direction_source` (`auto` | `employee`), yalnız INSERT.
- **Faz C — eksik yarı talebi + müdür tek tık onay** (kullanıcının §9 onayı: sonuç ekranında
  *"buton YOK"* kuralına istisna). Talep kayıt değildir: `punch_claims`; onay → `channel='manual'`
  kayıt + `audit_log`.

## Brief iddialarının doğrulaması (2026-10-21, `0e7c158`)

| Brief iddiası | Kodda | Sonuç |
|---|---|---|
| `resolveDirection` penceresiz toggle; `StaleOpenIn` yalnız not | `decide.go:533-544`, `:481` | ✅ doğru |
| Kök sorgu `GetLastOpenTransaction` | `transactions.sql:16-84` | ✅ doğru; ⚠️ ama sorgu **değişmek zorunda değil** — kural Decide'da. Yalnız *"şu ana göre"* yerine *"dokunuşun anına göre"* açık giriş için bir `@at` üst sınırı alır (M15-02, gerekçe ADR 0032 §A.4) |
| `anomalies.sql` `StaleOpenIn`'i kullanıyor | `anomalies.sql:650` | ❌ **yalnız bir yorumda** geçiyor. `ListOpenCheckIns` (`:644-710`) *"sonrasında hiç `out` yok"* der, `StaleOpenIn` okumaz. `StaleOpenIn`'in gerçek okuyucuları: `ledger/report.go:168` (`closingTail`), `:905` (`Stale`), `ledger/floor.go:122`, `ledger/tape.go:110, :178`, `ledger/site.go:164` (not metni) |
| Ledger de aynı kurala uymalı mı? | `ledger/report.go:865-1000` | ⚠️ **Evet — ve anomali listesi ile talep kapısı da, tek bir yüklemle** (ADR 0032 §A.6). `accumulate` sıralı eşler ve 18 sa'i aşan çifti açık sayar (`:980-988`) ama iş gününü bilmez: Faz B'de çalışanın seçtiği `out` önceki iş gününün girişiyle eşleşir ve 18 sa altındaysa **ödenir** (Pzt 15:00 + Sal 07:00 = 16 sa). Liste bugünkü tanımıyla aynı girişi kapalı sayar, talep kapısı onu reddederdi → 0 saat, müdüre hiçbir şey. Kart M15-05 (yüklem, Faz A) ve M15-09 (yetim çıkış) |
| `tap.Decide` saf kalır, saat dilimi/DST çağıranda | `decide.go:423-446`, `tzdata.go` | ⚠️ **kısmen yanlış öncül.** Decide **bugün de** saat dilimi çözüyor: geç kalma `shiftStartInstant` içinde `time.LoadLocation` yapar, `time/tzdata` gömülü — saflık bundan bozulmuyor. Plan çağırana **önhesaplı UTC an** değil **eşdeğerini** (kesim + bölge) verdirir ve hesabı `tap` paketinde **tek dışa açık saf fonksiyonda** yapar (ADR 0032 §A.3) |
| Hangi saat dilimi (tenant mı lokasyon mu) | `00001:34`; `locations`'ta sütun yok | Yalnız `tenants.timezone`. ⚠️ Q01 *"`locations.timezone` override'ı"* kararlaştırmıştı ama **hiç uygulanmadı**. Holding üyeleri K-36 ile aynı saat dilimini taşır. Öneri: **tenant (işveren) saat dilimi** |
| Tap ekranı metin dili | `tap.templ:117-123`, `view.go:389-399` | İngilizce: düğme bugün **"Tap"** (yönden bağımsız, bilinçli — `tap.templ:12-17`), sonuç *"Tapped in / Tapped out"* |
| `occurred_at` istemci beyanı, `sys:occurred-at-bound` 72 sa, M9-01 | `guardrails.go:79`, `baseline.go:229` | ✅; ek: `base:queued-window` **120 sn** üstü beyanı `flag`'e çeker. ⚠️ M9-01 (çevrimdışı kuyruk) **todo**; ama `occurred_at` form alanı `POST /api/checkin`'de **bugün canlı** (`handler/checkin.go:194`) |
| Sonuç ekranında buton yok | `result.templ:8-13` | ✅ |
| Mevcut onay kuyruğu `transaction_reviews` | `00005:260-356` | Yalnız `verdict='flag'` satırına karar yazılabilir (tetikleyici) — talepler oraya **giremez**; ayrı tablo şart (M15-15) |
| ADR 0011 *"düzeltme yalnız kısaltır"* çatışıyor mu | ADR 0011, `manual.go:85-147` | Q18'in kendi vakası (açık `in` + müdürün `out`'u) **kusursuz** — çıkış talebi tam bu. Giriş talebi yalnız arada yanlış bir `in` varsa tuzağa düşer; talep penceresi bunu dışarıda bırakır (M15-16) |
| Advisory kilit (ADR 0006) | `transactions.sql` `LockEmployeeForTap` | ✅; talep yazımı ve onayı da aynı kilidi alır |
| ADR 0031 boş · son migration | `docs/adr/0030-*`, `00063_*` | ❌ **0031 alınmış** — `main`'de boş ama `origin/docs/m14-identity-plan` (`2e593ef`, kabul `573731d`) `docs/adr/0031-tek-kimlik-cok-isletme.md`'yi taşıyor. Bu ADR **0032** (bütün dallarda boş, ölçüldü). Son migration `00063` ✅ |

**Brief'te olmayan, okurken bulunanlar** (ayrıntı ADR 0032 *"Bulunan riskler"*):

1. **Üç okuyucu, üç "açık" tanımı.** `ListOpenCheckIns` / `CountMaskedOpenCheckIns` *"açık"*ı
   `NOT EXISTS (sonrasında herhangi bir out)` diye tanımlar (`anomalies.sql:701-708`) — Faz A'nın
   *"Pzt `in`, Sal `in`, Sal `out`"* şeklinde Sal'ın çıkışı Pzt'nin unutulmuş girişini listeden siler.
   Ledger sıralı eşler ve 18 sa'i aşanı açık sayar ama iş gününü bilmez. Talep kapısı bir üçüncüsünü
   yazacaktı. Çözüm **tek yüklem** (ADR 0032 §A.6): *bir `out`, açık bir `in`'i kapatır ⇔ `out.At −
   in.At ≤ StaleOpenIn` ve (`out.business_day_start` NULL ya da `in.At ≥ out.business_day_start`)*;
   bir `in` açıktır ⇔ sonraki ilk yönlü olay onu kapatan bir `out` değil. Kapatmayan bir `out` **dört
   ayrık sınıftan** tam birindedir, her biri tek sayaca gider (çok uzun → `TooLong`; başka iş günü ve
   önsüz-S-dolu → yetim, `OutWithoutIn`; önsüz-S-NULL → `StartedEarlier`) — tarihsel satırlar bugünkü
   üç dalda kalır. Go'da tek sınıflandırma fonksiyonu, SQL'de tek fonksiyon, bir parite testi. Kartlar
   M15-05 (Faz A) ve M15-09.
2. **KM'nin vardiyaları 05:00 varsayılanıyla çakışıyor** (seed, `test/fixtures/seed.sql:195-211`):
   *KM Pastry & Bakery* 04:00–12:00 (05:00 vardiyanın **içinde**: 03:58 giriş önceki iş günüdür,
   12:05 çıkış `in` olur), *KM Meat Production* 05:00–13:00 (04:50'de gelen erken çalışan önceki
   iş gününe düşer). Faz A, KM'nin kesimi ayarlanmadan canlıya çıkarsa KM'de zinciri **bugünkünden
   kötü** bozar. Q30, kart M15-07.
3. **Form ve `tap.js` kaydı düşürebilir.** (a) `form.submit()` submitter'ın `name`/`value`'sunu
   göndermez; (b) konum API'si olmayan tarayıcıda betik doğal gönderime izin verir ama `markPending`
   düğmeyi **eşzamanlı** `disabled` yapar ve devre dışı submitter değer göndermez
   (`web/static/js/tap.js:67-71`, `:97-103`); (c) HTML'de statik bir gizli `want` alanı JS'siz telefonda
   **iki** `want` üretir — *"bozuk alan → 400"* kuralıyla birlikte bu bir **kayıt kaybıdır** (§4.6).
   Çözüm ADR 0032 §B.3: statik gizli alan yok, betik alanı dinamik ve `markPending`'den önce oluşturur,
   sunucu `want`'ı asla 400'e çevirmez (yorumlanamazsa *"istek yok"*). Kartlar M15-11, M15-12.
4. **Onaylı talepler API akışına girmez.** `punch_events_channel CHECK (channel IN ('nfc','qr'))`
   (`00048`) — müdürün yazdığı satır (talep onayı dahil) `/v1` punch akışında yok (K-6). Çalışanın
   seçtiği yön ise **işaretsiz** gider. Q31.
5. **Eski davranışı pinleyen testler.** Yeni 18 sa kuralı kesim **yokken de** geçerli olduğu için
   *"stale → `out`"* şeklini pinleyen testler kırmızıya döner — envanter M15-03 ve M15-04'te.
6. **Çok sıkı CHECK §4.6 kaybıdır.** `transactions`'a eklenen bir CHECK kod yolunda bir kez
   tutmazsa INSERT 23514 ile düşer → HTTP 500, **kayıt yok** (aynı şekil `types.go:364-373`'te
   `transactions_tag_fk`'nin 23503'ü için ölçülmüş). Yeni sütunların kısıtları yalnız sözlük ve
   tutarlılık kontrolüdür ve **ikinci** emniyettir: `insertParams` yönsüz kararda iki sütunu kodda NULL'a
   zorlar (M15-04, M15-11).
7. **`Down` kanıt siler.** `business_day_start` yeniden hesaplanamaz (kesim değişmiş olabilir — bu
   yüzden donduruluyor), `direction_source` hiç hesaplanamaz, talepler beyan kaydıdır. Üç `Down` dolu
   değer/satır varsa **reddeder** — emsal `00043_record_a_tap_on_a_peers_plaque.sql:430-433`.

## Çalışma modu (CLAUDE.md §10) — her kart için

- **Yapıcı + ayrı üçüncü göz.** Her kartı bir yapıcı alt ajan (opus) uygular; işi yapmayan **ayrı**
  bir denetçi kabul kriterlerini tek tek doğrular; bulgu → düzeltme → **yeni** denetçi. Kartta
  **"Mercek: iki"** yazıyorsa (şema, karar motoru, güvenlik sınırı) ayrıca agent
  `tappa-security-auditor`.
- **Testler:** tur içinde yalnız değişen testler `-run` ile (ör. `go test ./internal/domain/tap -run
  'TestDecide_'`) + saniyelik kapılar (`gofmt`, build, `bash scripts/redline-check.sh`). Tam
  `-race`, `go test ./...`, `make check` bir kartta **bir kez**, commit'ten önceki son doğrulamada;
  fazın büyük testleri **faz kapanış kartında** (M15-07, M15-14, M15-19). Yapıcı ve denetçi
  brief'ine bu yazılır.
- **`make gen`** her `.templ` / `db/queries/*.sql` değişikliğinden sonra; üretilen dosyalar commit
  edilir. **`make css`** her `input.css` değişikliğinden sonra.
- **Kapsam:** `internal/domain/tap` %90+ (CLAUDE.md §8) — Faz A ve B'nin motor kartları bunu düşürmez.
- **Dal:** feature dalı (ör. `m15-is-gunu`). `main`'e birleştirme bir **deploy kararıdır** —
  kullanıcı istemedikçe yok. Fazlar **ayrı ayrı** sevk edilebilir: A tek başına çığı keser.
- **Kart bitince:** `state.md` ledger + "ŞU AN" + oturum notu (ana oturum).

## Her kartın miras aldığı kısıtlar

- **Yön hesaptır, politika değildir** (CLAUDE.md §5). Gün dönümü bir **ayar**dır; `internal/policy`'e
  bağlam anahtarı, guardrail ya da baseline eklenmez. `policy_context`'e yön, iş günü ya da istenen
  yön **girmez** (`types.go:573-580`: kararı beslemeyen değerler bağlamda yoktur).
- **`tap.Decide` saf:** saat yok, DB yok, HTTP yok; `time.LoadLocation` gömülü tzdata ile serbest
  (bugünkü geç kalma hesabıyla aynı).
- **Tek temsil.** İş günü sınırı **tek** bir dışa açık saf fonksiyondan gelir (`tap` paketi); ledger,
  talep penceresi, ayar uyarısı ve `GET /t` tahmini onu çağırır. *"Bir çıkış bir girişi kapatır mı"*
  sorusu ve kapatmayan çıkışın **sınıfı** tek bir sınıflandırmadır: Go'da bir fonksiyon, SQL'de bir
  fonksiyon, aralarında bir parite testi
  (ADR 0032 §A.6). Kopya aritmetik yok (repo bunun bedelini `MinutesLate`, `StaleOpenIn`, `TrustBase`
  için ödedi).
- **Zaman UTC** (CLAUDE.md §6); yerel saate yalnız render ve kesim çözümü bakar. Kesim bir **duvar
  saati**dir: `time.Date(…, loc)` ile kurulur, gece yarısı + süre ile değil (DST).
- **`transactions` yalnız INSERT** (CLAUDE.md §4.3). Yeni sütunlar tarihsel satırlarda `NULL`'dır;
  geri doldurma yok. `Down`'lar dolu bir kanıt sütununu ya da talep satırını düşürmeyi **reddeder**.
- **Kayıt asla kaybolmaz** (CLAUDE.md §4.6): yeni kısıtlar kayıt yazımını hiçbir kod yolunda
  düşüremez (yukarıda risk 6); bir tercih alanı (`want`) hiçbir zaman 400'e dönüşmez (risk 3); talep
  akışının hiçbir hatası yazılmış dokunuşu etkilemez.
- **Tenant izolasyonu** (CLAUDE.md §4.5): her yeni tablo §6'nın beşlisiyle (tenant_id, ENABLE + FORCE
  RLS, politika, indeks, GRANT) doğar; her sorgu açık `tenant_id` filtresi taşır; holding'de zincir,
  kesim ve talep **işverenindir** (ADR 0027 §7).
- **GPS** (CLAUDE.md §4.2): yalnız tap anında, `tap.js`'in **tek** `getCurrentPosition` çağrısıyla.
  `GET /t` tahmini ve sonuç ekranındaki talep kartı GPS **okumaz**.
- **Log** (CLAUDE.md §7): oturum token'ı, imzalı bağlam, e-posta loglanmaz; talep satırları yalnız
  opak id'lerle loglanır.
- **Tap/sonuç ekranı** (CLAUDE.md §9, skill `tappa-brand`): yalnız kullanıcının 2026-10-21 onayının
  kapsadığı iki değişiklik (B: tahmini yön düğmesi + düzeltme bağlantısı; C: talep kartı). Başka her
  değişiklik yeniden sorulur. Tenant accent'i yalnız birincil tap düğmesinin zeminidir; düzeltme
  bağlantısı ve talep kartı accent **taşımaz**.

## Migration ve ADR numaraları (önceden dağıtıldı)

| No | Kart | İçerik | `Down` |
|---|---|---|---|
| **00064** | M15-02 | `tenants.day_cutoff` (NOT NULL DEFAULT `'05:00'`), `departments.day_cutoff`, `employees.day_cutoff` (NULL = kademede üstten devral), `transactions.business_day_start` (NULL tarihsel/manuel), ortak çıkış sınıflandırmasının SQL fonksiyonu | NULL olmayan bir `business_day_start` varsa **reddeder**; kesim sütunları (yapılandırma, geçmişi `audit_log`'da) düşer |
| **00065** | M15-08 | `transactions.direction_source` (`auto` \| `employee`, NULL tarihsel/manuel/yönsüz) | NULL olmayan bir değer varsa **reddeder** |
| **00066** | M15-15 | `punch_claims` + `punch_claim_decisions` (append-only, §6 beşlisi) | iki tablodan birinde satır varsa **reddeder** |

**Çakışma — ölçüldü (2026-10-21, `git ls-tree` bütün yerel ve uzak dallarda):** ADR **0031**
`origin/docs/m14-identity-plan`'da alınmış (`docs/adr/0031-tek-kimlik-cok-isletme.md`, kabul `573731d`),
bu yüzden M15'inki **0032**. M14 migration numarası **ayırmıyor** (`docs/plan/m14-tek-kimlik.md` yer
tutucu `000NN` kullanıyor) ve hiçbir dalda `00064`+ yok. Kural: M14'ün kodu M15'ten önce `main`'e
girerse M15 kendi migration'larını yeniden numaralar (dosya adı + `goose` sırası + bu tablo + ADR
atıfları aynı commit'te); yayımlanmış (canlıya çıkmış) bir migration'ın numarası asla değişmez
(CLAUDE.md §3). M14'ün dalı `roadmap.md`'ye M13'ün altına bir satır ekliyor; M15 satırıyla metin
düzeyinde birleştirme çakışması olur, içerik çakışması yok.

## Görev listesi

| ID | Faz | Görev | Araç | Mercek | Bağımlılık |
|---|---|---|---|---|---|
| M15-01 | A0 | ADR 0032'nin kabulü + kapsam ve Q30'un yanıtı | ana oturum + kullanıcı | iki | — |
| M15-02 | A | Şema 00064: gün dönümü kademesi, `business_day_start`, çıkış sınıflandırmasının SQL fonksiyonu; etkin kesim okuması; açık girişin `@at` sınırı | agent `tappa-db-migrator` | iki | M15-01 |
| M15-03 | A | Tap motoru: iş günü kuralı, `BusinessDayStart`, çıkış sınıflandırmasının Go fonksiyonu, yeni not; CLAUDE.md §5 | yapıcı | iki | M15-01 |
| M15-04 | A | Orkestratör: kesimi topla, iş günü anını yaz; DB testleri (Rusty Bar, fabrika, holding) | yapıcı | iki | M15-02, M15-03 |
| M15-05 | A | **Tek eşleme yüklemi** her okuyucuda: anomali listesi, maske sayısı, ledger, şerit; parite testi | yapıcı + agent `tappa-db-migrator` | iki | M15-04 |
| M15-06 | A | Panel: kesim ayarları (Account / departman / çalışan) + vardiya çakışma uyarısı + `audit_log` | yapıcı (`tappa-brand`) | iki | M15-02, M15-03 |
| M15-07 | A | Faz A kapanışı: seed + fixture, tam doğrulama, canlı çakışma raporu, deploy notu | denetçi + güvenlik | iki | M15-02…06 |
| M15-08 | B | Şema 00065: `transactions.direction_source` | agent `tappa-db-migrator` | iki | M15-07 |
| M15-09 | B | Yetim çıkış: anomali listesi + rapor sayacı (fixture S'yi ve kaynağı doğrudan yazar) | yapıcı + agent `tappa-db-migrator` | iki | M15-05, M15-08 |
| M15-10 | B | Tap motoru: istenen yön, `DirectionSource`, `PredictDirection`; CLAUDE.md §5 ikinci cümle | yapıcı | iki | M15-08 |
| M15-11 | B | `POST /api/checkin`: `want` alanı (asla 400), kilit altında karar, yazım | yapıcı | iki | M15-09, M15-10 |
| M15-12 | B | `GET /t` tahmini + tap ekranı (§9) + `tap.js` + Account önizlemesi | yapıcı (`tappa-brand`) | iki | M15-11 |
| M15-13 | B | Panel/rapor görünürlüğü: *"chosen by employee"* işareti | yapıcı (`tappa-brand`) | bir | M15-11 |
| M15-14 | B | Faz B kapanışı (+ gerçek tarayıcıda `want` doğrulaması) | denetçi + güvenlik | iki | M15-08…13 |
| M15-15 | C | Şema 00066: `punch_claims` + `punch_claim_decisions` + RLS testi | agent `tappa-db-migrator` | iki | M15-14 |
| M15-16 | C | Domain: talep penceresi, gönderim, karar (onay → manuel kayıt + audit, tek işlem) | yapıcı | iki | M15-15 |
| M15-17 | C | Sonuç ekranı talep kartı (§9 istisnası) + `POST /api/claim`; CLAUDE.md §5 *"Unutulan çıkış"*, §9; skill | yapıcı (`tappa-brand`) | iki | M15-16 |
| M15-18 | C | Panel: talep kuyruğu (Approve / Edit / Reject), anomali satırında *"claim pending"* | yapıcı (`tappa-brand`) | iki | M15-16 |
| M15-19 | C | Faz C kapanışı | denetçi + güvenlik | iki | M15-15…18 |

**Saat şişirme yolunun sırası (ADR 0032 §B.5).** Yolu açan şey arayüz değil `POST`'un `want` alanıdır
(M15-11). Onu kapatan ortak yüklem Faz A'da (M15-05) sevk edilir; yetim çıkışı görünür kılan liste ve
sayaç (M15-09) M15-11'in bağımlılığıdır. Yani `want` alanı, ondan doğan her şeklin rapor ve anomali
karşılığı **hazır** olmadan var olmaz.

---

## Faz A0 — Karar

## M15-01 — ADR 0032'nin kabulü

- **Bağımlılık:** —
- **Kırmızı çizgi:** §4.3, §4.6, §4.5, §4.2 (ADR'nin analizi), §5 (normatif metin), §9 (tap/sonuç ekranı)
- **Araç:** ana oturum; kullanıcı onayı · **Mercek: iki** (üçüncü göz ADR'yi kodla karşılaştırır; güvenlik merceği kırmızı çizgi analizini)
- **Commit:** `docs(adr): accept the business day and the direction chain`

**Amaç.** ADR 0032'yi *Önerildi*'den *Kabul edildi*'ye geçirmek; açık soruları (Q30, Q31, Q32)
yanıtlatmak.

**Neden.** Şema, karar motoru ve iki güvenlik sınırı (yeni POST yüzeyi, müdür yazma yolu) değişiyor
(CLAUDE.md §10: ADR şart). ADR brief'ten **sapan** üç öneri taşıyor ve kullanıcının görmesi gerekiyor:
(1) dondurulmuş `transactions.business_day_start` (§A.5) ve onu okuyan ortak eşleme yüklemi (§A.6),
(2) her iki düğmenin de **açık bir yön** göndermesi (§B.3), (3) yorumlanamayan `want`'ın 400 değil
*"istek yok"* sayılması (§B.3).

**Adımlar.** Üçüncü göz ADR'nin her `file:line` iddiasını HEAD'de yeniden okur; güvenlik merceği
kırmızı çizgi bölümünü denetler; bulgular işlenir; kullanıcı Q30 (kesim değerleri), Q31 (API alanı),
Q32 (talep sonucu bildirimi) için karar verir; ADR durum satırı ve tarihi güncellenir.

**Kabul kriterleri.**
- ADR'nin durum satırı *Kabul edildi (tarih)*, karar veren satırında kullanıcının kararları.
- Q30–Q32 `open-questions.md`'de *cevaplandı* ya da bilinçli olarak *açık* (hangi kartı bloklar
  yazılı).
- `TestEveryNamedTestExists` yeşil (ADR yalnız var olan testleri adlandırır).
- CLAUDE.md **bu kartta değişmez** — her normatif paragraf onu uygulayan kodla aynı commit'te
  değişir, çünkü §5 *"kod bundan sapıyorsa kod yanlıştır"* der:

  | CLAUDE.md paragrafı | Kart |
  |---|---|
  | §5 *"Yön (in/out)"* — iş günü kuralı | M15-03 |
  | §5 *"Yön (in/out)"* — çalışanın seçimi (ikinci cümle) | M15-10 |
  | §5 *"Unutulan çıkış"* — talep cümlesi | M15-17 |
  | §9 tap ekranı — tahmini yön + düzeltme bağlantısı | M15-12 |
  | §9 onay ekranı — talep kartı istisnası | M15-17 |
  | §3 dizin haritası — `internal/domain/claim/` | M15-16 |

**Tuzaklar.** ADR'yi kodu okumadan onaylamak (brief'in iki öncülü ölçümde yanlış çıktı) · CLAUDE.md'yi
burada değiştirmek.

---

## Faz A — İş günü (çığı keser)

## M15-02 — Şema: gün dönümü kademesi, iş günü anı ve eşleme yüklemi (00064)

- **Bağımlılık:** M15-01
- **Kırmızı çizgi:** §4.3 (`transactions`'a sütun, geri doldurma yok, `Down` kanıt silmez), §4.5 (kolon yetkileri, sorgu filtresi), §4.6 (kısıt kaybı), §6
- **Araç:** agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(db): add the business day cutoff and freeze the business day of each tap`

**Amaç.** Gün dönümü kademesinin üç sütunu, her yönlü dokunuşun iş günü başlangıcını donduran
`transactions.business_day_start`, ortak çıkış sınıflandırmasının SQL fonksiyonu, tap okumasında etkin kesim
ve *"şu ana göre"* yerine *"dokunuşun anına göre"* açık giriş.

**Neden.** Motor (M15-03) kesimi `Input`'ta, orkestratör (M15-04) onu tek okumada ister. İş günü
anı satırda **donar**, çünkü kesim sonradan değişebilir ve bir okuyucu geçmişi bugünkü ayarla yeniden
hesaplarsa geçmişin eşleşmesi değişir — `00016`'nın fatura dönemlerinde saat dilimini dondurma
gerekçesinin aynısı. Yüklem fonksiyonu M15-05'in sorgularıdır.

**Dokunulacak dosyalar.** `db/migrations/00064_add_the_business_day.sql`, `db/queries/employees.sql`
(`GetEmployeeForTap`), `db/queries/transactions.sql` (`GetLastOpenTransaction`, `InsertTransaction`),
`db/queries/tenants.sql` / `departments.sql` / `employees.sql` (ayar yazma sorguları — M15-06
kullanır), `internal/store/*` (üretim), DB testleri.

**Adımlar.**
1. `tenants.day_cutoff time NOT NULL DEFAULT '05:00'`; `departments.day_cutoff time` ve
   `employees.day_cutoff time` (NULL = devral). Üçünde de `CHECK (date_part('second', day_cutoff) = 0)`
   (dakika hassasiyeti). Var olan tenant satırları DEFAULT'u alır — `ALTER … ADD COLUMN … NOT NULL
   DEFAULT` PG 11+ meta-veri işlemidir, satır yeniden yazmaz.
2. **`tenants` UPDATE kolon kolon verilmiştir:** `00024`'ün `Up`'ı bugünkü listeyi verir
   (`00024:120` — `GRANT UPDATE (name, business_type, timezone)`; `:135`'teki beşli liste o
   migration'ın `Down`'ıdır). Bu migration `GRANT UPDATE (day_cutoff) ON tenants TO tappa_app` ekler.
   `departments` / `employees` tablo düzeyinde UPDATE'e sahip (`00002:102`, `00003:105`) — ek GRANT
   gerekmez. **Ölçüldü (2026-10-21):** hiçbir sonraki migration bu iki tabloda UPDATE'i daraltmıyor
   (`00003:106` yalnız `DELETE`'i geri alıyor; `00037` grant'ları koruyor).
3. `transactions.business_day_start timestamptz` (NULL). Tek kısıt:
   `CHECK (business_day_start IS NULL OR type IS NOT NULL)` — yönsüz (reject/ignored) satır iş günü
   taşımaz; bu kısıt **ikinci emniyettir**, birincisi `insertParams`'ın NULL'a zorlaması (M15-04).
   *"Yönlü her nfc/qr satırda dolu"* şartı **konmaz** (risk 6: bölge yüklenemezse motor hesaplayamaz
   ve kayıt 23514 ile düşerdi). `NOT VALID` + sonradan `VALIDATE` deseni de **kullanılmaz**.
4. **Çıkış sınıflandırması** (ADR 0032 §A.6): `public` şemada tek fonksiyon (ad yapıcının; ör.
   `tappa_checkout_class(prev_in_at timestamptz, out_at timestamptz, out_day_start timestamptz)
   RETURNS text` — `prev_in_at` NULL = çıkışın önündeki ilk yönlü olay bir `in` değil) —
   `LANGUAGE sql IMMUTABLE`, `SECURITY INVOKER`, **tablo okumaz**; beş değer: `closes`, `too_long`
   (a), `other_day` (b), `orphan` (c), `unanchored` (d). Sıra ADR'deki gibi: önce önsüz mü (c/d), sonra
   S koşulu (düşerse **(b)**, 18 sa'e bakılmaz — a ∧ b → b), sonra 18 sa (`out_at - prev_in_at <=
   interval '18 hours'` → `closes`, değilse `too_long`). *"Kapatır"* sorgularda `… = 'closes'`'tur.
   **Yetki deseni (ölçüldü):** `REVOKE EXECUTE … FROM PUBLIC` (`00061:98-102`'nin biçimi) ve
   `GRANT EXECUTE … TO tappa_app` (`00003:216-217`, `00004:182-183`'ün biçimi); fonksiyon `op_` ya da
   `holding_` öneki taşımadığı ve `SECURITY INVOKER` olduğu için `internal/db/operatorschema_test.go:501-517`'nin
   iki katalog kontrolüne (op_* sahibi, SECURITY DEFINER sahibi) **girmez**. Süre literali
   `tap.StaleOpenIn`'in kopyasıdır ve M15-05'in parite testi ikisini bağlar.
5. `GetEmployeeForTap`: `LEFT JOIN departments d ON d.id = e.department_id AND d.tenant_id =
   e.tenant_id` ve `coalesce(e.day_cutoff, d.day_cutoff, t.day_cutoff) AS effective_day_cutoff`.
   Açık `tenant_id` filtresi korunur.
6. `GetLastOpenTransaction`'a `@at timestamptz`: dış satırda `t.occurred_at <= @at`, `NOT EXISTS`
   içinde `o.occurred_at <= @at`. Canlı dokunuşta `@at = now` → sonuç **bugünküyle aynı** (gelecek
   damgalı satır `sys:occurred-at-bound` ile `reject`'tir, yönü yoktur). Geçmiş beyanlı bir
   dokunuşta (M9-01 kuyruğunun şekli) *"o an açık olan giriş"*i döndürür (ADR 0032 §A.4). Sorgunun
   uzun yorum bloğu (`NOT t.practice`, maliyet ölçümü) korunur; yeni paragraf `@at`'in nedenini yazar.
   Bu sorgu eşleme yüklemini **kullanmaz** (ADR 0032 §A.6 neden gerekmediğini söyler; M15-05 ölçer).
7. `InsertTransaction`: `business_day_start` parametresi. **Eş mekân dokunuşu da bu sorguyu
   kullanır** (`internal/domain/checkin/checkin.go:1559-1568`, `site_*` sütunları dolu); ayrı bir
   *"site kardeşi"* yalnız manuel yazımdadır (`InsertManualSiteTransaction`, `transactions.sql:393`)
   ve manuel satır `business_day_start` yazmaz.
8. **`Down`:** önce `transactions.business_day_start IS NOT NULL` olan bir satır varsa `RAISE
   EXCEPTION … USING ERRCODE = 'object_not_in_prerequisite_state'` ile **reddeder** (hiçbir şey
   değişmez — `00043:430-433`'ün deseni); yoksa fonksiyonu, GRANT'ı, kısıtı ve dört sütunu düşürür.
9. Migration testi: varsayılan değer, kısıt, kolon yetkisi (`tappa_app` `tenants.day_cutoff`'u
   günceller, `vat_number`'ı hâlâ güncelleyemez), sınıflandırma fonksiyonunun beş sınıfı ve sınırları, `Down`'ın dolu
   bir satırla reddi.

**Kabul kriterleri.**
- `-- +goose Down` dolu; boş veritabanında geri alınabilir, dolu bir `business_day_start` varken
  **reddeder** ve hiçbir şeyi değiştirmez.
- `tappa_app` ile: kendi tenant'ının `day_cutoff`'unu günceller; başka tenant'ınkini RLS ile
  göremez; `00024`'ün kapalı tuttuğu kolonlar hâlâ kapalı.
- `GetEmployeeForTap` kademe testi: çalışan > departman > tenant.
- `GetLastOpenTransaction`: `@at = now` ile mevcut DB testleri (`TestGatherDB_` önekli olanlar)
  değişmeden yeşil; yeni test: geçmiş `@at` o an açık girişi döndürür, `@at`'ten sonraki bir `in`'i
  döndürmez.
- Yüklem fonksiyonu `SECURITY DEFINER` değil, tablo okumuyor (`redline-check.sh` ve katalog testi
  — SECURITY DEFINER yalnız `op_*`/holding/feed tanımlayıcılarına izinli).
- `transactions` tetikleyicileri (`tappa_forbid_mutation`, holding ve feed tetikleyicileri) yeni
  sütunla çalışır; `tappa_feedwriter` / `tappa_holdingdefiner` kolon yetkileri **genişlemez**.
- `bash scripts/redline-check.sh` yeşil.

**Tuzaklar.** `tenants` için tablo düzeyinde `GRANT UPDATE` vermek (`00024`'ün daralttığını açar) ·
`business_day_start`'a sıkı CHECK · `GetLastOpenTransaction`'ın `NOT t.practice` yüklemini düşürmek
(ADR 0008) · `@at`'i yalnız dış satıra koyup `NOT EXISTS`'e koymamak · `Down`'ı koşulsuz bırakmak ·
üretilen `internal/store` dosyalarını elle düzenlemek.

## M15-03 — Tap motoru: iş günü kuralı

- **Bağımlılık:** M15-01
- **Kırmızı çizgi:** §5 (normatif metin değişir), §4.6 (yön hiçbir kaydı düşürmez), CLAUDE.md §8 (%90+)
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(tap): close an open check-in only within its business day`

**Amaç.** `resolveDirection`'ı ADR 0032 §A kuralına geçirmek; iş günü sınırının ve ortak eşleme
yükleminin saf Go fonksiyonlarını dışa açmak.

**Neden.** Kök neden burada; geri kalan her kart bu fonksiyonların sonucunu taşır.

**Dokunulacak dosyalar.** `internal/domain/tap/types.go`, `decide.go`, `decide_test.go`,
`crosslocation_test.go`, `table_test.go`, yeni bir test dosyası (adı yapıcının), `CLAUDE.md` (§5),
`internal/domain/ledger/site.go` (`partnerKeptAnnotations`).

**Adımlar.**
1. `types.go`: `type DayCutoff struct { Cutoff time.Duration; Timezone string }` (Cutoff = yerel gece
   yarısından uzaklık, `Shift.Start` ile aynı kodlama) ve `Input.DayCutoff *DayCutoff`. `nil` = kesim
   yok → **yalnız `StaleOpenIn`** (M4 çağıranlarının ve mevcut testlerin şekli).
2. Dışa açık saf fonksiyon (ad yapıcının; ör. `BusinessDayStart(at time.Time, c DayCutoff)
   (time.Time, bool)`): `at`'in yerel günü için `time.Date(y, m, d, hh, mm, 0, 0, loc)`; sonuç
   `at`'ten sonraysa bir gün geri (`d-1`, `time.Date` normalize eder). Sınır anı yeni güne aittir
   (`at` = kesim → o günün başlangıcı). Boş ya da yüklenemeyen bölge → `ok=false` (geç kalmadaki
   `shiftStartInstant` kuralı: UTC'ye düşmez). Yanına bir sonraki iş gününün başlangıcı (talep penceresi).
3. Dışa açık saf **çıkış sınıflandırması** (ADR 0032 §A.6; ad yapıcının): `(prevInAt *time.Time,
   outAt time.Time, outDayStart *time.Time) CheckoutClass` — beş sınıf (`closes`, (a) çok uzun, (b)
   başka iş günü, (c) önsüz S dolu, (d) önsüz S NULL), SQL fonksiyonuyla aynı sıra (a ∧ b → b);
   *"kapatır"* yüklemi `== closes`. Motorun kuralı (adım 4) bununla **tutarlıdır** (motor `out` dediği
   her durumda sınıf `closes`'tur — özellik testi), ama motor onu çağırmaz: motorun sorusu *"bu dokunuş
   kapatmalı mı"*, sınıflandırmanın sorusu *"bu kayıtlı çıkış ne yaptı"*.
4. `resolveDirection`:
   - `at := tapInstant(in)` (beyan edilmişse `OccurredAt`, değilse `Now`) — **bugün `in.Now`**;
     değişiklik ADR 0032 §A.4'te gerekçeli.
   - açık giriş yok ya da practice → `in` (değişmez);
   - `open.OccurredAt.After(at)` (sıra dışı beyan) → `in`;
   - `at − open.OccurredAt > StaleOpenIn` → **`in`** + yeni not *(bugün: `out` + stale notu)*;
   - `DayCutoff` var, sınır hesaplandı ve `open.OccurredAt` sınırdan önce → `in` + yeni not;
   - aksi hâlde `out`.
5. `Decision`'a: iş günü anı (`*time.Time`, hesaplanamazsa `nil`; yalnız yönlü kararlarda) ve
   açık bırakılan giriş (`id` + neden: önceki iş günü | eski) — Faz C'nin kartı bunu okur.
6. Yeni not sabiti (dışa açık, ör. *"previous check-in left open (no checkout recorded)"* — metni
   yapıcı/denetçi kesinleştirir): yeni `in`'in hem `Note`'una hem `EmployeeNote`'una, bugünkü sırayla
   (*"kural; [başka şube;] yön"*) eklenir. **Eski `StaleOpenInNote` sabiti kalır** (tarihsel notlar;
   `ledger/site.go`'nun `partnerKeptAnnotations`'ı onu tanımak zorunda); yeni not da listeye eklenir.
7. `StaleOpenIn`'in yorum bloğu yeni anlamıyla yeniden yazılır (*"eşikten eski açık giriş artık
   kapatılmaz"*); `resolveDirection`'ın *"deliberately NO today's records window"* paragrafı ADR
   0032'ye atıfla yeniden yazılır; `Type`'ın doc'u (`types.go:83-87`) güncellenir.
8. **CLAUDE.md §5** *"Yön (in/out)"* maddesinin ilk paragrafı ADR 0032 *"CLAUDE.md metin
   önerileri"*ndeki metinle değişir (bu commit'te, kodla birlikte).

**Ters iddiayla yeniden yazılan testler (envanter, `0e7c158`).** Yeni 18 sa kuralı **kesim yokken
de** geçerlidir; şu testler *"stale → `out`"* şeklini pinler ve kırmızıya döner — her biri silinmez,
**ters iddiayla** yeniden yazılır (adlarını yapıcı korur ya da değiştirir; değiştirirse atıflar aynı
commit'te):
- `decide_test.go:597-611` (stale → `out` + not) ve `:613-634` (eşik sınırı) — yeni: stale → `in` +
  yeni not; eşiğin 1 dk altı aynı iş gününde `out`, 1 dk üstü `in`.
- `TestDecide_CrossLocationNoteSitsBetweenTheRuleAndTheDirection` (`crosslocation_test.go:197-211`) —
  not sırasını *stale `out`* üzerinden pinliyor; sıra (*"kural; başka şube; yön notu"*) **yeni notla**
  yeniden pinlenir (`ledger.partnerNote` yön notunu sonek olarak arar).
- `TestDecide_TheEmployeesNoteHasNoNoteEffect` (`crosslocation_test.go:213-237`) — aynı; çalışanın
  notu *"kural; yön notu"*, kaydın notu *"kural; başka şube; yön notu"*.
- Arandı ve **kırılmayanlar** (motoru stale açık girişle çalıştırmıyorlar): `crosslocation_test.go:134`
  (yalnız ön koşul: not yok), `table_test.go` (yalnız yorumda), `ledger/partnernote_test.go:36`,
  `ledger/holding_db_test.go:825-832`, `internal/handler/result_test.go:1018-1032`,
  `unbranded_golden_test.go:152` (sabit not metnini **görünüm girdisi** olarak kullanırlar; tarihsel
  satırın notunu çizmeye devam ederler). DB alt testi M15-04'te.

**Kabul kriterleri.** (`TestDecide_` önekli, tablo bazlı; adları yapıcı koyar)
- Yukarıdaki envanterdeki testler ters iddiayla yeniden yazıldı; **geri kalan bütün `TestDecide_`
  önekli testler değişmeden yeşil** (ör. `TestDecide_DirectionNilForNonRecordVerdicts`,
  `TestDecide_DirectionPracticeOpenInDoesNotCloseChain`, `decide_test.go:540-563` Rusty Bar testi).
- Aynı iş günü: Pzt 09:12 `in` → Pzt 17:30 → `out`.
- **Kullanıcının vakası:** Pzt 09:12 açık → Sal 09:05 → **`in`** + yeni not; açık bırakılan giriş
  Pzt'ninki; Sal 17:40 → `out` (açık giriş Sal 09:05).
- **Yalnız iş günü kuralının yakaladığı vaka** (18 sa'in altında): Pzt 16:00 açık (geç vardiya,
  unutulan çıkış) → Sal 09:00 (17 sa) → **`in`**. Kesimsiz (`DayCutoff == nil`) aynı vaka `out` verir —
  iki sonuç yan yana pinlenir (sabit pencerenin neden yetmediğinin testi).
- **Rusty Bar** (Europe/Malta, kesim 05:00, vardiya 18:00–02:00): Pzt 18:05 `in` → Sal 02:10 →
  `out`; Pzt 18:05 açık → Sal 17:55 → `in`.
- **Fabrika gece vardiyası** (departman 22:00–06:00): tenant kesimi 05:00 ile Pzt 22:00 → Sal 06:05
  → **`in`** (yanlış ayarın sonucu, testle **belgelenir**); departman kesimi 14:00 ile → `out`.
- Beyan edilmiş an: canlı `Now` Sal 06:00 iken `OccurredAt` Pzt 23:50 → iş günü Pzt'ye göre → `out`
  (açık giriş Pzt 18:00); `OccurredAtFromClient=false` iken `OccurredAt` alanı **okunmaz**.
- Sıra dışı beyan: açık giriş dokunuşun anından sonra → `in`.
- DST: Europe/Malta 2026-10-25 (03:00→02:00) ve 2027-03-28 (02:00→03:00) günlerinde kesim 05:00
  doğru UTC anına düşer; kesim 02:30 için sonuç **pinlenir** (Go'nun `time.Date` normalizasyonu).
- Boş ve bilinmeyen bölge → sınır yok → yalnız `StaleOpenIn` (UTC'ye **düşmez**).
- Sınıflandırma: beş sınıfın her hücresi + sınırlar (`in.At == S` → kapatır; tam 18 sa → kapatır; 18 sa
  + 1 sn → (a)) + (a ∧ b) → (b) + motor-sınıflandırma tutarlılık özellik testi.
- Reject / ignored / redirect hâlâ yönsüz ve iş günü anı taşımaz.
- Saflık: `go list -deps ./internal/domain/tap` DB/HTTP içermez; paket `time.Now` çağırmaz.
- Paket kapsamı ≥ %90.

**Tuzaklar.** `in.Now`'ı bırakıp yalnız sınırı eklemek (M9-01 kuyruğunun dünkü çıkışı bugünün iş
gününe düşer) · sınırı gece yarısı + `Cutoff` süresiyle kurmak (DST günü 23/25 sa) · eski not
sabitini silmek · kesim yokken sessizce 05:00 varsaymak (motor değer **uydurmaz**) · yön kuralını
`internal/policy`'e taşımak · not sırası testlerini *"yeni nota uydurmak için"* gevşetmek (sıra
yeniden **pinlenir**, iddia zayıflamaz).

## M15-04 — Orkestratör: kesimi topla, iş günü anını yaz

- **Bağımlılık:** M15-02, M15-03
- **Kırmızı çizgi:** §4.6 (her karar yazılır; yönsüz kararda yeni sütunlar kodda NULL), §4.5 (holding: işverenin kesimi), §4.3
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(checkin): judge the direction against the person's business day`

**Amaç.** `internal/domain/checkin` etkin kesimi ve saat dilimini `tap.Input.DayCutoff`'a, dokunuşun
anını `GetLastOpenTransaction`'ın `@at`'ine koyar; kararın iş günü anını satıra yazar.

**Dokunulacak dosyalar.** `internal/domain/checkin/checkin.go` (`gatherWith`, `gatherPerson`,
`insertParams`), `internal/domain/ledger/report.go` (yalnız `:131-169` yorum bloğu), DB testleri
(`internal/domain/checkin`, `internal/handler/checkin_db_test.go`, `internal/handler/day_db_test.go`).

**Adımlar.**
1. `gatherWith`: `emp.EffectiveDayCutoff` + `emp.TenantTimezone` → `tapFacts.dayCutoff`. **Eş
   mekânda da işverenin** kesimi ve saat dilimi (zincir işverenindir — ADR 0027 §7; K-36 iki
   tenant'ın aynı saat dilimini garanti eder). `site.Timezone` yalnız vardiya içindir, değişmez.
2. `gatherPerson`: `GetLastOpenTransaction(@at = occurredAt)` — `Record`'un hesapladığı `occurredAt`
   (beyan yoksa `now`). Kilit altında, aynı işlemde (ADR 0006).
3. `insertParams`: `dec.Type != nil` ise `dec`'in iş günü anı → `business_day_start`; **`dec.Type ==
   nil` ise `business_day_start` kodda NULL** (Faz B'de `direction_source` de — M15-11). CHECK ikinci
   emniyettir; bir kod hatası ona çarpıp kaydı 23514 ile düşürmemeli (§4.6).
4. `ledger/report.go:131-169` yorum bloğu: *"motor `in.Now − open.OccurredAt` ölçer"* cümlesi artık
   yanlış — motor da kayıt anını ölçüyor; blok düzeltilir (kural değişmez). **Bu düzeltme yalnız bu
   kartta.**

**Ters iddiayla yeniden yazılan DB testi:** `internal/handler/checkin_db_test.go:1808-1830` alt testi
*"ok out closing a stale check-in: the two-part note"* — 19 sa yaşında açık girişin ardından dokunuş
artık `in` + yeni not; alt testin iddiası (üretimden sürülen iki parçalı notun ekrana birebir çıkması)
korunur, yön ve not metni yeni kurala göre. `openCheckInAgedBy` yardımcısı (`:1855-1862`) değişmez.

**Kabul kriterleri.**
- DB testi, KF St Julians kesim 05:00: Pzt `in` açık bırakılır, Sal sabah dokunuşu `in` olarak yazılır,
  `business_day_start` Sal 05:00 yerel (UTC karşılığı), not yeni sabit.
- Rusty Bar gecesi uçtan uca: 18:05 `in`, 02:10 `out`, ikisinde de `business_day_start` aynı.
- Holding: KM çalışanı KF plaketinde — KM'nin kesimi uygulanır (KF'nin kesimi farklı ayarlanmış bir
  fixture ile ölçülür); `TestHoldingTap_DirectionChainStaysAtTheEmployer` yeşil.
- `TestSeedDB_ADayAtKFStJulians` ve `TestGatherDB_APracticeRowDoesNotHideAnOlderOpenCheckIn` yeşil
  (gerekirse fixture'ları açık kesimle güncellenir, iddiaları gevşetilmez).
- Reject/ignored satırlarında `business_day_start` NULL — **birim testiyle** `insertParams` düzeyinde
  (yönsüz bir `Decision`'a iş günü anı verilmiş olsa bile parametre NULL), DB testiyle satır düzeyinde;
  manuel satırlarda NULL.
- Mutasyon: `@at`'i `now`'a sabitlemek bir geçmiş-beyan testini kırmızıya çevirir; kesimi
  yutmak (`DayCutoff: nil`) kullanıcının *"17 sa"* vakasını kırmızıya çevirir; `insertParams`'taki NULL
  zorlamasını silmek yeni birim testini kırmızıya çevirir.

**Tuzaklar.** Eş mekânda sahanın kesimini okumak · kesimi kilitli bölümün dışında okumak (iki
okuma, iki anlık görüntü) · beyan edilmiş anı `@at`'e değil `now`'a koymak.

## M15-05 — Tek eşleme yüklemi her okuyucuda

- **Bağımlılık:** M15-04
- **Kırmızı çizgi:** §4.6 (sinyal kaybı — *"çıkış eksik"* listeden silinmez), bordro doğruluğu, §4.5 (sorgu filtresi)
- **Araç:** yapıcı (ledger, şerit) + agent `tappa-db-migrator` (sorgular + sqlc + DB testi) · **Mercek: iki**
- **Commit:** `fix(ledger): pair a checkout with a check-in by one rule everywhere`

**Amaç.** ADR 0032 §A.6'nın yüklemi — *bir `out` açık bir `in`'i kapatır ⇔ `out.At − in.At ≤
StaleOpenIn` ve (`out.business_day_start` NULL ya da `in.At ≥ out.business_day_start`); bir `in`
açıktır ⇔ kişinin ondan sonraki ilk yönlü, practice olmayan olayı (`(occurred_at, id)` sırasıyla) onu
kapatan bir `out` değildir* — ve kapatmayan bir `out`'un **dört ayrık sınıfı** (ADR 0032 §A.6
tablosu: (a) çok uzun → `TooLong`; (b) başka iş günü ve (c) önsüz-S-dolu → yetim; (d) önsüz-S-NULL →
`StartedEarlier`; a ∧ b → b) — **her** okuyucuda: `ListOpenCheckIns`, `CountMaskedOpenCheckIns`,
`accumulate`, şeridin eşlemesi (`tape.go`). Talep kapısı (M15-16) ve *"giriş eksik"* listesi (M15-09)
aynı sınıflandırmayı kullanır.

**Neden.** Bugünkü `NOT EXISTS (sonrasında herhangi bir out)` tek bir `out`'u **bütün** eski açık
girişleri kapatan bir şey sayar (`anomalies.sql:713-720` bunu practice maskesi için zaten söylüyor):
Faz A'da Sal'ın çıkışı Pzt'nin unutulmuş girişini listeden siler. Ledger iş gününü bilmez: Faz B'de
çalışanın seçtiği `out` önceki iş gününün girişiyle eşleşip **ödenir**. İki tanım, üçüncüsü talep
kapısında yazılacaktı; ve üçü farklı olursa (üçüncü göz vakası: Pzt 15:00 `in`, Sal 07:00 seçilmiş
`out`, S = Sal 05:00) ledger Pzt'yi açık sayar, liste kapalı sayıp düşürür, talep kapısı reddeder → 0
saat, müdüre hiçbir şey düşmez. Koruma `want` alanından (M15-11) **önce**, Faz A'da sevk edilir.

**Dokunulacak dosyalar.** `db/queries/anomalies.sql` (`ListOpenCheckIns`, `CountMaskedOpenCheckIns`
— ikisi de M15-02'nin SQL fonksiyonunu çağırır), `db/queries/transactions.sql`
(`ListWorkedShiftEvents` ve şeridin okuması `business_day_start`'ı döndürür),
`internal/domain/ledger/report.go` (`accumulate`), `tape.go`, `anomaly.go` (okuyucu ve ekran cümlesi),
M12-15 *"Açık kalanlar"* okuyucusu (aynı sorgu), DB testleri (`internal/handler/anomalies_db_test.go`,
`internal/domain/ledger`), yeni parite testi.

**Adımlar.**
1. `ListOpenCheckIns`: `NOT EXISTS` yerine *"sonraki olay"* (ör. `LATERAL (… ORDER BY occurred_at,
   id LIMIT 1)`), `type IS NOT NULL AND NOT practice`, açık `tenant_id` filtresi her alt sorguda; açık
   ⇔ sonraki olay yok, ya da `in`, ya da sınıfı `closes` olmayan bir `out` (sınıf, sonraki `out` için
   `prev_in_at` = bu `in` ile M15-02'nin fonksiyonundan).
2. `CountMaskedOpenCheckIns`: sıralı tanımla ADR 0008'in maskelenmiş girişleri **listede görünür**
   hâle gelir (*"closed_later"* kolu boşalır). Sorgu ve ekran cümlesi yeniden yazılır ya da sorgu
   emekliye ayrılır — yapıcı ölçer, denetçi onaylar; ekran yalan söylemez.
3. `accumulate`: bugünkü üç dal (`report.go:964-988`: `open == nil` → `StartedEarlier`; `worked >
   closingTail` → `TooLong` + `openEntry`; aksi hâlde süre) M15-03'ün sınıflandırma fonksiyonuyla
   seçilir: `closes` → süre; **(a)** → `TooLong` + `openEntry(in)` (bugünkü dal, çıkış girişi tüketir);
   **(d)** → `StartedEarlier` (bugünkü dal). S'si NULL bir satır yalnız bu üçüne düşer — **tarihsel
   raporlar bit bit aynı** (kriter). **(b)** → `openEntry(in)` ve açık slot **boşalır** (`open = nil`,
   bugünkü iki dalın yaptığı gibi) — böylece aynı kişinin sonraki ikinci `out`'u önsüz olur ve (c)
   sınıfına düşer, SQL listesinin *"önceki ilk olay"* sorusuyla aynı cevabı verir; **(c)** → hiçbir sayaç. (b) ve (c)'nin
   çıkış yarısının `OutWithoutIn`'e bağlanması M15-09'dadır; Faz A'da üretimde oluşamazlar (motorun
   `auto` çıkışı her zaman `closes`'tur — M15-03'ün özellik testi), yalnız fixture'la. Dönem sınırı
   kuralları ADR 0032 §A.6'daki gibi (S dönem başından önce olan bir (c) → `StartedEarlier` dikişi;
   başka dönemdeki açık `in` → bugünkü `continue`). `tape.go:178` aynı fonksiyonu çağırır.
4. **Yorumlar ve ekran:** `anomalies.sql:647-652`'nin *"THE DEFINITION IS … NOT THE REPORTS
   SECTION'S … the two numbers can legitimately differ"* bloğu yeniden yazılır (artık aynı tanım;
   kalan fark yalnız dönem sınırı — adım 6); anomali ekranında aynı iddiayı taşıyan cümle varsa
   güncellenir.
5. **Parite testi** (DB): beş sınıfın her hücresi (önsüz × S NULL/dolu; önlü × S NULL/içinde/dışında ×
   ≤/> 18 sa) ve sınırlar (`in.At == S`, tam 18 sa, 18 sa + 1 sn, a ∧ b) Go fonksiyonunda ve SQL
   fonksiyonunda değerlendirilir, **sınıflar** eşit; SQL fonksiyonunun süresi `pg_get_functiondef`'ten
   okunur ve `tap.StaleOpenIn` ile karşılaştırılır (`TestGuardrails_TagNotActiveIsAnAllowList`'in
   şema-kod bağı deseni).
6. **`GetLastOpenTransaction` neden sınıflandırmayı kullanmaz** (ADR 0032 §A.6): özellik testi —
   rastgele zincirlerde ve **sabit kesimle**, `closes` olmayan bir çıkışın arkasında kalan girişi motor
   sonraki hiçbir dokunuşta kapatmaz. **Kesim değişimi vakası** ayrıca: kesim iki dokunuş arasında
   05:00 → 14:00 değişince sorgu bir girişi gizleyebilir ve motor `in` der — test bu sonucu
   **muhafazakâr** sınır olarak pinler (çıkış uydurulmaz, süre yaratılmaz, giriş *"çıkış eksik"*
   listesinde kalır).
7. Maliyet: `EXPLAIN (ANALYZE, BUFFERS)` dev veritabanında önce/sonra; sayı belgede **tarihli
   gözlem** olarak yazılır, sınır diye değil.

**Kabul kriterleri.**
- DB testi: Pzt `in`, Sal `in`, Sal `out` → Pzt `in` listede, Sal `in` değil.
- DB testi (üçüncü göz vakası, satırlar fixture'la doğrudan yazılır — `direction_source` sütunu henüz
  yok, gerekmez): Pzt 15:00 `in`; Sal 07:00 `out`, `business_day_start` = Sal 05:00 → Pzt **listede**,
  ledger Pzt'yi `OpenEntry` sayar, 0 sa ödenir.
- Eski toggle şekli (Pzt 09:00 `in`, Pzt 17:00 `out`, S NULL) → listede değil, 8 sa (davranış korunur).
- Tarihsel *"stale `out`"* şekli (Pzt 09:00 `in`, Sal 09:05 `out`, S NULL, 24 sa) → sınıf (a): Pzt
  *"çıkış eksik"* listesinde (bugün değil — doğru yönde değişiklik, ekran cümlesi bunu söyler);
  ledger'da `TooLong` 1 ve `StartedEarlier` **bugünkü değerinde** (çıkış dikiş sayılmaz).
- **Tarihsel raporlar değişmez:** S'si NULL satırlardan kurulmuş bir hafta için `accumulate`'in bütün
  çıktısı (`Totals` ve kişi satırları) değişiklikten önceki çıktıyla bit bit aynı — eski ve yeni
  dallar aynı fixture'larda karşılaştırılır (ölçüm testi; geçmiş bir dev haftası üzerinde de bir kez).
- Faz B şekli fixture'la (S dolu): 05:30 `in` → ertesi 04:30 `out` (kesim 05:00, 23 sa) → yalnız (a),
  tek sayaç; Pzt 15:00 `in` → Sal 07:00 `out` (S Sal 05:00) → (b): Pzt açık, hiçbir sayaçta iki kez yok.
- **Açık slot (b)'den sonra boşalır** (fixture, S dolu): Pzt 15:00 `in`, Sal 07:00 `out`, Sal 08:00 `out`
  → Sal 07:00 (b), Sal 08:00 (c); ledger'ın sınıfları SQL sınıflandırmasıyla (her çıkış için önceki ilk
  olayla) satır satır aynı, Pzt girişi tek bir `OpenEntry`.
- Practice satırı sıradaki olay sayılmaz (`NOT practice`; ADR 0008).
- **Liste = rapor, ölçülebilir biçimde:** aynı tenant ve aynı yerel hafta için (listenin `[from, to)`'su
  = raporun `Period`'u), raporun `Open` girişleri (`OpenEntry`, `TooLong` yolundan gelenler dahil)
  listenin döndürdüğü girişlerle **aynı küme**; ölçüm dışı bırakılanlar adlandırılır: rapor sınırı
  aşıldığında (`ReportEventCap`), listenin kendi sınırı dolduğunda (`ListOpenCheckIns`'in
  `LIMIT @row_limit`'i, `anomalies.sql:710` — eski girişler önce gelir, fazlası kesilir), ve ledger'ın
  çalışana bağlayamadığı satırlar (`events`'in `unattributable` sayısı). Rapor dönem dışındaki girişleri
  raporlamaz (`report.go:889-891`) — bu, aynı pencereyi seçerek eşitlenir, kriter dışı değildir.
- ADR 0011'in tablosu (`internal/domain/ledger` `correction_test.go`) değişmeden yeşil (manuel satır,
  S NULL).
- Şerit (M12-14) ve rapor aynı haftada aynı aralıkları gösterir.
- RLS: tenant A'nın bağlantısı B'nin açık girişini görmez (mevcut izolasyon testleri yeşil).
- Parite testi yeşil; SQL fonksiyonunun süresini 19 sa'e çeviren mutasyon ve a ∧ b sırasını (a)'ya
  çeviren mutasyon kırmızı.

**Tuzaklar.** Sıradaki olayı yalnız `out` arasında aramak (yine *"herhangi bir out"* olur) · `id`
beraberlik bozucusunu unutmak · yüklemi sorgulara satır içi kopyalamak (fonksiyon çağrılır) ·
`closingTail` sabitini silmek (yorumu ve `ListWorkedShiftEvents`'in okuma ufku onu kullanıyor) · M12-15'in
yeniden kullandığı okuyucuyu ayrı bırakmak.

## M15-06 — Panel: kesim ayarları ve vardiya çakışma uyarısı

- **Bağımlılık:** M15-02, M15-03
- **Kırmızı çizgi:** §4.5 (yetki: tenant kesimi yalnız owner), §9 (panel — skill `tappa-brand`), §7 (log)
- **Araç:** yapıcı, skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(panel): set the business day cutoff and warn when it cuts a shift`

**Amaç.** Üç ayar yüzeyi: **Account** (tenant varsayılanı; Account bugün owner'a açık —
ölçüldü: `internal/handler/accountactions.go:96` `mayEditAccount` — `id.Admin.Role == adminRoleOwner`), **departman formu** (override, boş =
devral), **çalışan formu** (override, boş = devral; etkin değer ve kaynağı gösterilir). Kesim bir
vardiyayı keserse **kaydetmeden önce** uyarı ve açık *"Save anyway"* onayı.

**Neden.** Varsayılan 05:00 her işletmeye uymaz: KM'nin fırını 04:00–12:00 (risk 2), fabrika gece
vardiyası 22:00–06:00. Yanlış kesim Faz A'yı o departmanda bugünkünden kötü yapar; ekran bunu
söylemeli.

**Adımlar.**
1. Saf çakışma fonksiyonu (`internal/domain/tenant`; ad yapıcının): bir kesim, bir vardiyanın
   **`(başlangıç − 1 sa, bitiş + 2 sa]`** aralığına (gece vardiyasında gece yarısını aşan aralık)
   düşüyorsa çakışır. **Uçlar** (ADR 0032 §A.7): sınır anı yeni iş gününe aittir; bu yüzden alt uç
   **hariç** (kesim tam `başlangıç − 1 sa` ise en erken beklenen giriş kesimin kendisidir, yeni güne
   düşer — çakışma yok) ve üst uç **dahil** (kesim tam `bitiş + 2 sa` ise en geç beklenen çıkış yeni
   güne düşer ve `in` olur — çakışma). Paylar adlandırılmış sabitler (erken gelen / geç çıkan;
   değerler kartın önerisi, pilotta ayarlanır). Sınır kontrolü `tap`'in iş günü fonksiyonuyla aynı
   duvar saati kuralını kullanır.
2. Account kaydında: kesim, **kendi kesimi olmayan** her departmanın ve departmansız çalışanların
   kullandığı her lokasyonun vardiyasıyla karşılaştırılır. Departman kaydında: departmanın vardiyası.
   Çalışan kaydında: çalışanın çözülmüş vardiyası (departman > lokasyon).
3. Çakışma listesi uyarıda adlarıyla (departman/lokasyon adı, vardiya saatleri); kayıt ikinci
   POST'ta (onay alanıyla) yapılır — panelin mevcut onay deseni (ör. `deactivateconfirm.go`) izlenir.
4. Her değişiklik `audit_log`'a: eylem adı mevcut sözlükten türetilir (`<isim>.<geçmiş zaman>`),
   `detail`'de eski/yeni değer ve kapsam (tenant/departman/çalışan id) — kişisel veri yok.
5. Metinler İngilizce; panel bileşenleri M12'nin `Panel*` ailesi; tek yeni sınıf varsa `input.css`
   + `make css`.

**Kabul kriterleri.**
- Owner tenant kesimini kaydeder; manager Account'a bugün erişemiyorsa yine erişemez (mevcut testler
  yeşil). Departman/çalışan formu bugün kimlere açıksa onlara açık (yetki genişlemez).
- Kesim 05:00 ile (seed vardiyaları): Rusty Bar 18:00–02:00 → **yok**; KF 10:00–22:00 → yok;
  *KM Central Kitchen* 06:00–14:00 → **yok** (alt uç hariç: 05:00 = başlangıç − 1 sa);
  *KM General* 09:00–17:00 → yok; *KM Warehouse & Supply* 08:00–16:00 → yok; *KM Meat Production*
  05:00–13:00 → **uyarı**; *KM Pastry & Bakery* 04:00–12:00 → **uyarı**; fabrika 22:00–06:00 →
  **uyarı**, departman kesimi 14:00 ile → yok. Üst uç: 10:00–22:00 vardiyası ve kesim 00:00 →
  **uyarı** (= bitiş + 2 sa, dahil).
- Uyarı kaydı engellemez (onayla kaydedilir) ve onaysız ilk POST **hiçbir şey yazmaz**.
- `audit_log` satırı her kayıtta tam bir kez.
- Başka tenant'ın departman/çalışan id'si ile POST → bugünkü form davranışı (0 satır, tanımlı ret).

**Tuzaklar.** Uyarıyı yalnız departman vardiyasına bakarak yapmak (departmansız çalışanların lokasyon
vardiyası da kesimi kullanır) · gece vardiyası aralığını düz karşılaştırmak · uçları ters kurmak ·
*"boş = 00:00"* okumak (boş = devral).

## M15-07 — Faz A kapanışı

- **Bağımlılık:** M15-02 … M15-06
- **Kırmızı çizgi:** hepsi (faz diff'i)
- **Araç:** ayrı denetçi + agent `tappa-security-auditor`; skill `tappa-seed` · **Mercek: iki**
- **Commit:** `test(seed): give each demo business a business day` (+ yalnız düzeltme commit'leri)

**Amaç.** Fazın büyük testleri bir kez, seed'in yeni kurala uyması, canlıya çıkıştan önce çakışma
raporu.

**Adımlar.**
1. Seed (`test/fixtures/seed.sql`, skill `tappa-seed`'in gün simülatörü): KF kesimi 05:00; KM
   kesimi **Q30'un cevabı** (öneri 23:00); simülatör unutulan çıkış üretiyorsa yeni kuralla doğru
   yönleri ürettiği ölçülür. **`tags` satırlarına dokunulmaz** (plaket anahtarı kuralı).
2. Tam `make check` (fmt + lint + `-race`), `make audit`; güvenlik merceği bütün Faz A diff'inde.
3. **Canlı çakışma raporu (salt okunur):** üretim veritabanında her tenant için M15-06'nın çakışma
   kuralının SQL karşılığı ya da `cmd` altında salt okunur bir sorgu — yalnız tenant/departman adı ve
   vardiya saatleri; kişisel veri yok. Çıktı kullanıcıya verilir; çakışan tenant'ın kesimi deploy'dan
   **önce** (ya da migration'la aynı pencerede, owner Account'tan) ayarlanır.
4. **Deploy ve geri dönüş notu** (`state.md`, ana oturum): migration 00064'ün `Down`'ı yalnız
   **hiç `business_day_start` yazılmamışken** çalışır (ilk dokunuştan önce); sonrasında reddeder,
   çünkü S yeniden hesaplanamaz — kesim değişmiş olabilir, donmasının nedeni tam olarak bu (ADR 0032
   §A.5, §*"Geri alma"*). İlk dokunuştan sonra geri dönüş **binary'yi** geri almaktır: eski kod yeni
   sütunları yazmaz (NULL) ve okumaz, CHECK NULL'a izin verir, sınıflandırma fonksiyonunu çağıran yok.

**Kabul kriterleri.**
- Tam suit yeşil (`-race`); `make audit` exit 0; `TestEveryNamedTestExists` yeşil.
- Güvenlik merceği Faz A diff'inde ONAY.
- Seed yüklendikten sonra `TestSeedDB_ADayAtKFStJulians` ve KM'nin bir günü (fırın 04:00–12:00 dahil)
  doğru yönlerle geçer.
- Canlı çakışma raporu kullanıcıya sunuldu; Q30 cevaplandı.
- `Down`'ın dolu satırla reddi dev veritabanında bir kez elle ölçüldü (çıktı rapora yazılır).
- `main`'e birleştirme kullanıcının kararı.

---

## Faz B — Tahmini yön ve düzeltme

## M15-08 — Şema: `transactions.direction_source` (00065)

- **Bağımlılık:** M15-07
- **Kırmızı çizgi:** §4.3 (`Down` kanıt silmez), §4.6 (kısıt kaybı), §6
- **Araç:** agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(db): record who chose the direction of a tap`

**Amaç.** `direction_source text` — `CHECK (direction_source IN ('auto','employee'))` ve
`CHECK (direction_source IS NULL OR type IS NOT NULL)` (ikinci emniyet; birincisi `insertParams`,
M15-11). `InsertTransaction` parametresi (eş mekân dokunuşu da aynı sorgu); manuel INSERT'ler yazmaz.
`ListWorkedShiftEvents` ve yetim çıkış sorgusu (M15-09) sütunu döndürür.

**Tarihsel satırlar:** **NULL**, geri doldurma yok (CLAUDE.md §4.3'ün ruhu: değişmez satıra yeni bir
iddia eklenmez). Okuyucu kuralı: yönlü bir `nfc`/`qr` satırında NULL = **M15 öncesi, motorun
seçimi** (o tarihten önce düzeltme yolu yoktu); `manual` satırda yön müdürün yazdığıdır ve `channel`
bunu zaten söyler; yönsüz satırda NULL anlamsızdır. Bu kural tek yerde (okuyucu yardımcı fonksiyonu)
yazılır.

**`Down`:** NULL olmayan bir `direction_source` varsa **reddeder** (`00043:430-433` deseni) — çalışanın
seçimi yalnız o satırda yazılıdır ve hiçbir yerden yeniden hesaplanamaz.

**Kabul kriterleri.** Boş veritabanında `Down` geri alır, dolu bir değerle reddeder ve hiçbir şey
değiştirmez; `'manager'` gibi üçüncü bir değer reddedilir; yönsüz satıra değer yazılamaz;
tetikleyiciler ve feed yazıcısının kolon yetkileri genişlemez; *"her yönlü nfc/qr satırda dolu"*
CHECK'i **yok** (risk 6).

**Tuzaklar.** `DEFAULT 'auto'` koymak (yönsüz satırlarla çelişir, tarihsel satırlara iddia ekler) ·
`NOT VALID` + sonradan `VALIDATE` (tarihsel NULL'lar) · koşulsuz `Down`.

## M15-09 — Yetim çıkış: anomali listesi ve rapor sayacı

- **Bağımlılık:** M15-05, M15-08
- **Kırmızı çizgi:** §4.6 (saat kaybolmaz, ayrı gösterilir), bordro doğruluğu, ADR 0011, §4.5
- **Araç:** yapıcı (ledger) + agent `tappa-db-migrator` (sorgu) · **Mercek: iki**
- **Commit:** `feat(ledger): count a checkout that closes nothing`

**Amaç.** Sınıflandırmanın (ADR 0032 §A.6) yetim sınıflarını — **(b)** başka iş günü ve **(c)**
önsüz-S-dolu — tek bir rapor sayacına ve bir *"giriş eksik"* listesine bağlamak. Faz B'de çalışanın
seçtiği `out` bu şekilleri günlük olarak üretebilir; `want` alanı (M15-11) bu kart **olmadan** var
olmaz.

**Neden.** M15-05'ten sonra (b) ve (c) çıkışları hiçbir sayaca girmiyor (Faz A'da üretimde
oluşamadıkları için). Dönem içinde başlamış bir iş gününün yetim çıkışı bir dikiş (`StartedEarlier`,
*"vardiya dönemden önce başladı"* — ADR 0011'in *"iki kez yanlış"* dediği cümle) değil, *"giriş eksik"*
anomalisidir. **Her çıkış tam bir sayaçtadır:** (a) yalnız `TooLong`'da, (b)/(c) yalnız
`OutWithoutIn`'de, (d) yalnız `StartedEarlier`'da.

**Adımlar.**
1. `accumulate`: (b) ve (c) → `Totals.OutWithoutIn` (ad yapıcının) + kişi/gün işareti; (b)'de giriş
   M15-05'teki gibi `openEntry`. Dönem sınırı ADR 0032 §A.6'daki gibi: S'si dönem başından önce olan bir
   (c) → `StartedEarlier` dikişi (önündeki giriş okunmamış olabilir). (a) ve (d) **dokunulmaz**.
2. Anomali: *"giriş eksik"* listesi — yeni sorgu `anomalies.sql`'e; bir `out`, kişinin ondan önceki ilk
   yönlü, practice olmayan olayıyla M15-02'nin fonksiyonuna verildiğinde sınıfı `other_day` ya da
   `orphan` ise listelenir — yani **S'si NULL hiçbir satır listede yoktur** ve (a) sınıfı da yoktur. Açık
   `tenant_id` filtresi her alt sorguda; pencere ve `LIMIT @row_limit` `ListOpenCheckIns`'in desenidir;
   ekranda *"çıkış eksik"* listesinin yanında.
3. Fixture'lar `business_day_start`'ı ve `direction_source`'u doğrudan yazar — motorun Faz B yolu
   (M15-10/11) bu kartın bağımlılığı değildir.

**Kabul kriterleri.**
- **(b)** Pzt 15:00 `in` (auto, S Pzt 05:00) + Sal 07:00 `out` (employee, S Sal 05:00) → 0 sa ödenir;
  Pzt *"çıkış eksik"* listesinde ve ledger'da açık; Sal 07:00 *"giriş eksik"* listesinde ve
  `OutWithoutIn`'de **1**; `TooLong` ve `StartedEarlier` **0**.
- **(a ∧ b)** Pzt 09:00 `in` + Sal 09:05 `out` (employee, S Sal 05:00, 24 sa) → (b): `OutWithoutIn` 1,
  `TooLong` 0, Pzt açık.
- **(a), Faz B** 05:30 `in` → ertesi 04:30 `out` (employee, kesim 05:00, S koşulu sağlanıyor, 23 sa) →
  `TooLong` 1, `OutWithoutIn` 0, *"giriş eksik"*te yok.
- **(c)** önünde `in` olmayan bir `out` (employee, S dönem içinde) → `OutWithoutIn` 1, `StartedEarlier`
  0; S dönem başından önceyse → `StartedEarlier` 1, `OutWithoutIn` 0.
- **Tarihsel stale şekil** (Pzt 09:00 `in`, Sal 09:05 `out`, S NULL, 24 sa) → (a): `TooLong` ve
  `StartedEarlier` **bugünkü değerinde**, `OutWithoutIn` 0, satır *"giriş eksik"*te **yok**.
- **(d)** S NULL, önünde `in` olmayan tarihsel `out` → `StartedEarlier` bugünkü değerinde, listede yok.
- Pzt 09:00 `in` + Pzt 17:00 `out` (employee, aynı iş günü) → 8 sa (`employee` aynı günü kapatabilir).
- **Çift sayım yok:** her fixture'da `TooLong + OutWithoutIn + StartedEarlier + kapanan çiftler` =
  dönem içi çıkış sayısı (sınır kuralları hariç tutulmuş hâliyle) — bir değişmez testi.
- S'si NULL satırlardan kurulmuş bir hafta için `accumulate` çıktısı M15-05 öncesiyle bit bit aynı
  (M15-05'in ölçüm testi bu kartta da yeşil).
- ADR 0011'in tablosu ve *"kapatılamaz açık kayıt"* tablosu (`correction_test.go`) değişmeden yeşil.
- *"Giriş eksik"* listesi RLS ve açık filtreyle tenant'a kapalı.

**Tuzaklar.** Yetim çıkışı `StartedEarlier`'a da yazmak (çift sayım) · listede S süzgecini unutmak
(bütün tarihsel stale çıkışlar listeye dolar) · (a ∧ b)'yi (a) saymak (talep kapısıyla ayrılık) · kesimi
bugünkü ayardan yeniden hesaplamak · sınıflandırmayı bu sorguda satır içi yeniden yazmak.

## M15-10 — Tap motoru: istenen yön ve tahmin

- **Bağımlılık:** M15-08
- **Kırmızı çizgi:** §5 (normatif metin), §4.6, CLAUDE.md §8
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(tap): honour the direction the employee chose`

**Amaç.** `Input.RequestedDirection *Type` (nil = istek yok), `Decision.DirectionSource`
(`auto`|`employee`) ve dışa açık saf tahmin fonksiyonu (ör. `PredictDirection(open *Transaction, at
time.Time, c *DayCutoff) Type`) — `resolveDirection` **onu çağırır** (tek temsil; `GET /t` aynı
fonksiyonla tahmin eder).

**Kural (ADR 0032 §B.3).** Motor yönü `D`'yi her zamanki gibi hesaplar. İstek yoksa ya da istek `D`
ise → `D`, `auto`. İstek `D`'den farklıysa → istenen yön, `employee`, `Note` ve `EmployeeNote`'a
yeni not (*"direction chosen by employee"*). Yalnız `ok`/`flag`'te (yönü olan kararlar); reject,
ignored, redirect'te istek **düşer**, `DirectionSource` boş. **Verdict, güven puanı ve geç kalma
istekten etkilenmez** — ama geç kalma kaydedilen yöne göredir (istenen `in` geç kalma ölçer, istenen
`out` ölçmez). Çalışan `in` seçip aynı iş günündeki açık girişi açık bırakırsa açık bırakılan giriş
`Decision`'da *"çalışan seçimi"* nedeniyle işaretlenir (Faz C kartı bu nedende **açılmaz**);
çalışan `out` seçip kapatılabilir giriş yoksa karar *"giriş eksik"* (yetim çıkış) olarak işaretlenir
(Faz C'nin giriş talebi).

**Kabul kriterleri.** (`TestDecide_` önekli, tablo bazlı)
- Dört hücre: tahmin `in`/istek yok · tahmin `in`/istek `out` · tahmin `out`/istek `in` · tahmin
  `out`/istek `out` → kaynak ve not doğru.
- `flag` kararında istek uygulanır, verdict `flag` kalır; `reject`/`ignored`'da yön ve kaynak yok.
- İstenen yön politika bağlamına **girmez** (`PolicyContext` anahtar kümesi değişmez — mevcut
  bağlam testleri yeşil).
- `PredictDirection` ile `resolveDirection` aynı girdide aynı yönü verir (özellik testi).
- Motorun ürettiği her (`want` × durum) kaydı için ortak yüklem M15-09'un okumasıyla tutarlı: seçilmiş
  bir `out` yalnız kendi iş gününün girişini kapatır.
- CLAUDE.md §5'in *"Yön"* maddesine Faz B cümlesi (ADR 0032) bu commit'te eklenir.

**Tuzaklar.** İsteği verdict'e bağlamak · `employee`'yi *"istek geldi"* diye yazmak (istek `D` ile
aynıysa `auto`'dur — değişmez: `employee` ⇔ kaydedilen yön ≠ kilit altında hesaplanan `D`) · tahmin
için ikinci bir kural yazmak.

## M15-11 — `POST /api/checkin`: istenen yönün yolu

- **Bağımlılık:** M15-09, M15-10
- **Kırmızı çizgi:** §4.6 (tercih alanı kaydı asla düşürmez), §4.5, ADR 0006 (kilit), §7
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(checkin): carry the employee's chosen direction to the decision`

**Amaç.** Form alanı `want`, kilitli bölümde `tap.Input.RequestedDirection`, satırda
`direction_source`. **Hiçbir `want` biçimi 400 değildir** (ADR 0032 §B.3): bütün değerler toplanır,
boşlar atılır; hiç kalmazsa → istek yok; kalanların hepsi aynı ve `in`/`out` ise → o yön; çelişen ya da
tanınmayan değer → istek yok + bir uyarı log satırı (değer sözlükte değilse metni loglanmaz). Bozuk
`occurred_at`'in 400'ü (`handler/checkin.go:194-199`) bilinçli olarak izlenmez: `want` yalnız bir
tercihtir ve güvenli bir yorumu vardır — motorun kararı.

**Adımlar.** 1) handler: `r.PostForm["want"]` → yorum fonksiyonu (saf, tablo testli); 2)
`checkin.Request` alanı; 3) `Record` → `Input`; 4) `insertParams` → `direction_source`, **`dec.Type ==
nil` iken kodda NULL** (M15-04'ün `business_day_start` kuralının eşi); 5) `Result`'a kaynak (sonuç
ekranı notu `EmployeeNote`'tan gelir); 6) `ledger/site.go` `partnerKeptAnnotations`'a yeni not (eş
mekân satırında not korunur — kanıt anlatmaz); 7) `devtap.go` (geliştirme aracı) `want`'ı geçirebiliyorsa
aynı alan; 8) M9-01 (todo) kartına not: kuyruk `want`'ı da saklar.

**Kabul kriterleri.**
- DB testi: tahmin `in` iken `want=out` → satır `type=out`, `direction_source=employee`, `verdict=ok`,
  not yeni sabit; `want` yokken → `auto`.
- **JS'siz POST (handler testi, gerçek router):** tarayıcının doğal gönderimi tek bir `want` taşır →
  doğru yazılır. Ayrıca her biri **tam bir** satır yazar: `want=` (boş) → `auto`; `want=out&want=out`
  → `out`; `want=in&want=out` → motorun yönü, `auto`, uyarı logu; `want=sideways` / `want=OUT` →
  motorun yönü, `auto`, uyarı logu (log satırında değerin kendisi yok). Hiçbir vaka 400 değil.
- **Eşzamanlılık** (`-race`): bir kişi için `want=out` ve `want` yok iki eşzamanlı POST → advisory
  kilit sırayla karar verdirir; biri `ignored` (60 sn debounce), öteki yazılır; hiçbir satırda
  `direction_source` kilit altındaki `D` ile çelişmez.
- **Bayat sayfa:** sayfa `out` tahmin etti, POST'tan önce iş günü sınırı geçti (`D=in`), kişi ana
  düğmeye bastı (`want=out`) → `out`, `employee`; satır M15-09'un yüklemine göre yetim çıkış.
- Yönsüz kararda (`ignored`) `direction_source` NULL — birim testiyle `insertParams` düzeyinde.
- Eş mekân: KM çalışanı KF plaketinde `want` ile → işverende yazılır, `site_punches` yönü işverenin
  satırındaki yöndür (K-40); `TestEvidenceReaders_EveryOneIsClassified` yeşil.

**Tuzaklar.** `want`'ı imzalı bağlama koymak (gerek yok — ADR 0032 §B.3) · yorumlanamayan `want`'ı
reddetmek (fiziksel dokunuş kaybolur) · istenen yönü kilit dışında uygulamak.

## M15-12 — `GET /t` tahmini ve tap ekranı (§9)

- **Bağımlılık:** M15-11
- **Kırmızı çizgi:** §9 (tap ekranı — kullanıcı onayı 2026-10-21), §4.2 (GPS yalnız basışta), §4.6 (form kaydı düşürmez), §4.5 (tahmin okuması işverende)
- **Araç:** yapıcı, skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(tap): say the predicted direction on the tap button`

**Amaç.** `GET /t` tahmini yön için bir okuma yapar (açık giriş `@at = now` + etkin kesim + saat
dilimi → `tap.PredictDirection`); tap ekranı:

- Birincil düğme: *"Clock in"* / *"Clock out"* — `<button type="submit" name="want" value="in|out"
  class="tap-button" data-tap-button>`. Accent slotu **bu** düğmedir (ADR 0023 §2 değişmez).
- Altında metin bağlantısı görünümlü ikinci gönderim: *"Not right? Clock out"* / *"… Clock in"* —
  `<button type="submit" name="want" value="…">`, ink, altı çizili, `min-h-11`, accent **yok**.
- **HTML'de statik gizli `want` alanı yok** (JS'siz telefonda ikinci bir `want` üretirdi).
- Tahmin okuması başarısızsa ya da yapılamıyorsa: bugünkü **"Tap"** düğmesi, `name`/`value` yok, ikinci
  bağlantı yok (motor karar verir, `auto`). Okuma hatası loglanır, sayfa düşmez.

**Adımlar.**
1. `tenant.Directory.TapPage` / `PeerTapPage` → `TapPageFacts`'e tahmin (eş mekânda **işverenin**
   zinciri). Tek okuma, sayfanın mevcut işlemi içinde.
2. `pages.TapView`'e tahmin; `tap.templ` (`tapForm`, `TapButtonFace`); `tap.templ:12-17`'nin
   *"THE BUTTON IS DIRECTION-NEUTRAL"* yorumu ADR 0032'ye atıfla yeniden yazılır.
3. **`tap.js`** (ADR 0032 §B.3): `submit` işleyicisinin **ilk işi** — `submitting` korumasından sonra,
   `markPending`'den ve konum dalından **önce** — basılan düğmenin değerini (`event.submitter`; yoksa
   düğmelerin `click` işleyicisinin sakladığı değer) **dinamik oluşturulan** gizli bir `want` alanına
   yazmak; sonra **iki** düğmeyi birlikte devre dışı bırakmak (*"Tapping…"* yalnız basılan düğmede).
   Böylece hem `form.submit()` yolu hem konum API'siz doğal gönderim yolu (`tap.js:97-103`; devre dışı
   submitter değer göndermez) tam bir `want` taşır. **Tek gizli alan:** formda betiğin işaretli gizli
   `want` alanı (ör. bir `data-` özniteliğiyle) zaten varsa **yeniden kullanılır ve değeri güncellenir**,
   yenisi eklenmez — bfcache'ten geri yüklenen sayfa ya da ikinci bir gönderim iki alan üretmez
   (sunucu iki eşit değeri yine tek sayar, ama betik ona güvenmez). GPS çağrısı **tek** kalır.
4. Account önizlemesi (`TapButtonFace(TapButtonPreview)`, M10 WL-7): önizleme *"Clock in"* yüzünü
   çizer, bağlantıyı çizmez; hâlâ `type="button"`, form dışı, `name`/`value` yok.
5. Goldens: tap ekranının bayt-aynı golden'ları (`TestUnbrandedScreens_AreByteIdenticalToTheGolden`
   kapsamındaki tap render'ları) bu §9 kararıyla **bilinçli** yeniden üretilir — diff kart raporunda
   satır satır; sonuç ekranı ve aktivasyon ailesinin golden'ları **değişmez**.
6. CLAUDE.md §9 (*"tek ekran, tek buton"* paragrafı) ve skill `tappa-brand` *"Tap ekranı — kutsal
   alan"*: ADR 0032'deki istisna cümlesinin tap ekranı yarısı (bu commit'te).

**Kabul kriterleri.**
- Üç şekil render testiyle: tahmin `in`, tahmin `out`, tahmin yok (bugünkü bayt dizisi). Hiçbir şekilde
  `<input type="hidden" name="want"` yok (render testi).
- Tek birincil düğme (`tap-button` sınıfı tam bir kez); düzeltme bağlantısı accent sınıfı/değişkeni
  taşımaz (`TestTapPage_TheBrandFillsTheTwoSlotsAndNothingElse` genişletilir, gevşetilmez).
- **`tap.js` için kaynak düzeyi test** — repoda bilinçli olarak JS çalışma zamanı yok (Node yasak,
  `internal/handler/tap_test.go:719-725`'in açıkladığı sınır); `TestTapScript_DoublePressCannotEscapeAsANativeSubmit`'in
  deseninde (sunulan betiği okur, biçimi pinler): `event.submitter` (ve `click` yedeği) okunuyor;
  gizli `want` alanı **dinamik** oluşturuluyor ve **önce mevcut olanı arıyor** (varsa yeniden kullanıp
  değerini güncelliyor — oluşturma koşulsuz değil); bu yazım işleyicide **`markPending`'den ve konum
  dalından önce** geliyor ve **`form.submit()`'ten önce**; iki düğme de devre dışı bırakılıyor; mevcut
  çift basma testi yeşil. Testin kendi yorumu bunun davranış kanıtı değil biçim ağı olduğunu söyler —
  davranış M15-14'te gerçek tarayıcıda.
- `getCurrentPosition` sayfa başına basışta tam bir kez (§4.2; kaynak düzeyi); `GET /t` hiçbir konum
  alanı okumaz.
- Eş mekân plaketinde tahmin işverenin zinciri (fixture: KF'de açık giriş, KM plaketinde *"Clock
  out"*).
- 390 / 834 / 1200 px'te yatay kaydırma yok; ikinci hedef ≥ 44 px; kontrast AA.

**Tuzaklar.** Bağlantıyı `<a href>` yapmak (GET ile kayıt yazılmaz; GPS ve imzalı bağlam gitmez) ·
ikinci bir `<form>` · statik gizli alan · gizli alanı `markPending`'den sonra yazmak (konum API'siz yol)
· accent'i bağlantıya taşımak · tahmini sonuç ekranının metniyle yarıştırmak.

## M15-13 — Panel ve rapor görünürlüğü

- **Bağımlılık:** M15-11
- **Kırmızı çizgi:** §9 (panel), §4.5 (eş mekân satırı — K-39 kanıt kuralı)
- **Araç:** yapıcı, skill `tappa-brand` · **Mercek: bir** (okuma ve görünüm; yeni yazma yok)
- **Commit:** `feat(panel): show which directions the employee chose`

**Amaç.** İşlem listesinde ve fişte (`docket`) *"chosen by employee"* işareti (not zaten fişte; ek
olarak mono bir etiket), raporda kişi/gün işareti ve sayı (`DayMarks`'ın yanında), CSV dışa aktarımda
bir sütun (`direction_source`, tarihsel NULL → okuyucu kuralı M15-08).

**Kabul kriterleri.** Üç yüzeyde de işaret; eş mekân satırında işaret görünür ama kanıt kuralı
(K-39) değişmez; `TestEvidenceReaders_EveryOneIsClassified` yeşil; CSV başlığı değişiyorsa CSV
testleri aynı commit'te.

## M15-14 — Faz B kapanışı

- **Bağımlılık:** M15-08 … M15-13 · **Araç:** ayrı denetçi + agent `tappa-security-auditor` · **Mercek: iki**
- **Kırmızı çizgi:** hepsi (faz diff'i) — özellikle §4.2 (`tap.js`), §4.6 (`want`), bordro (yüklem)
- **Commit:** yalnız düzeltme commit'leri

**Adımlar.** Tam `make check`, `make audit`; güvenlik merceği bütün Faz B diff'inde (yeni form alanı,
tap ekranı, `tap.js`, ledger sayacı). 390/834/1200 px ekran görüntüsü turu (tap ekranının üç şekli).
**Gerçek tarayıcıda elle doğrulama** (repoda JS çalışma zamanı olmadığı için tek davranış kanıtı):
dev sunucuda, tarayıcının ağ panelinde `POST /api/checkin` gövdesi okunur — dört vaka: JS açık + konum
izni var (ana düğme ve bağlantı), JS açık + konum API'si yok ya da reddedildi, JS kapalı. Q31'in
cevabına göre API notu.

**Kabul kriterleri.**
- Tam suit yeşil (`-race`); `make audit` exit 0; `TestEveryNamedTestExists` yeşil.
- Güvenlik merceği Faz B diff'inde ONAY.
- Elle doğrulamanın dört vakasında da gövdede **tam bir** `want` ve basılan düğmenin değeri; her
  vakada tam bir satır yazıldı ve `direction_source` beklenen değer (ekran görüntüleri ya da ağ
  kaydının metni rapora eklenir).
- Ekran görüntüsü turu: üç şekil × üç genişlik, yatay kaydırma yok.
- Q31 cevaplandı ya da bilinçli olarak açık (karar rapora yazılı).
- `main`'e birleştirme kullanıcının kararı.

---

## Faz C — Eksik yarı talebi

## M15-15 — Şema: `punch_claims` ve `punch_claim_decisions` (00066)

- **Bağımlılık:** M15-14
- **Kırmızı çizgi:** §4.5, §6 (beşli), §4.3'ün kardeşi (append-only — ADR 0009 deseni; `Down` kayıt silmez), §4.7
- **Araç:** agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(db): add punch claims and their decisions`

**Amaç.** İki append-only tablo (ADR 0032 §C.2):

- `punch_claims`: `id`, `tenant_id NOT NULL`, `employee_id NOT NULL` (bileşik FK `employees(id,
  tenant_id)`), `anchor_transaction_id NOT NULL` (bileşik FK `transactions(id, tenant_id)`), `kind`
  (`in` | `out` — **eksik olan** yarı), `claimed_at timestamptz NOT NULL`, `created_at DEFAULT
  now()`. `UNIQUE (tenant_id, anchor_transaction_id)` — kayıt başına bir talep.
- `punch_claim_decisions`: `id`, `tenant_id`, `claim_id` (bileşik FK, `UNIQUE`), `outcome`
  (`approved` | `rejected`), `decided_by NOT NULL` (admin), `approved_at timestamptz` (onayda dolu;
  *Edit* edilmişse talepten farklı), `transaction_id` (onayda yazılan manuel satır, bileşik FK),
  `note`, `decided_at DEFAULT now()`; `CHECK ((outcome = 'approved') = (transaction_id IS NOT NULL
  AND approved_at IS NOT NULL))`.
- İkisi de: `ENABLE` + `FORCE ROW LEVEL SECURITY`, `NULLIF(current_setting('app.tenant_id', true),
  '')::uuid` politikası, `tenant_id` önde indeks (+ kuyruk için `(tenant_id, created_at)`),
  `GRANT SELECT, INSERT` → `tappa_app`, `REVOKE UPDATE, DELETE`, `tappa_forbid_mutation` tetikleyicisi
  (00005'in deseni).
- Sorgular: talep INSERT'i **çapa satırından kurulur** (`INSERT … SELECT … FROM transactions WHERE
  tenant_id = @tenant_id AND id = @anchor AND employee_id = @employee`) — yabancı ya da başkasının
  çapası 0 satır; çapanın hâlâ eksik olup olmadığı **M15-02'nin sınıflandırma fonksiyonuyla** (komşu olaylar
  okuması); bekleyen kuyruk listesi; karar INSERT'i.
- **`Down`:** iki tablodan birinde satır varsa **reddeder** (`00043:430-433` deseni); talepler ve
  kararları beyan ve karar kaydıdır.

**Kabul kriterleri.**
- RLS izolasyon testi (filtresiz sorgu ile — CLAUDE.md §6): A, B'nin talebini ve kararını görmez.
- UPDATE/DELETE `tappa_app`'ta yetki hatası, `tappa_owner`'da tetikleyici hatası (00005'in ölçüm
  biçimi).
- İkinci talep aynı çapaya → unique ihlali; ikinci karar aynı talebe → unique ihlali.
- Çapa başka çalışanın → 0 satır; başka tenant'ın → 0 satır.
- `scripts/redline-check.sh` R5 iki tabloyu tenant tablosu sayar ve geçer;
  `scripts/pg-restore-verify.sh` ENABLE/FORCE sayıları eşit.
- `Down` boş tablolarda geri alır, bir satır varken reddeder ve hiçbir şey değiştirmez.

**Tuzaklar.** Tek tabloda `status` UPDATE'i (onay kararı geri alınabilir hâle gelir — ADR 0009) ·
oturum token'ını ya da hash'ini talep satırına yazmak (bağ imzalı bağlamla kurulur, M15-17) ·
`transaction_reviews`'e talep yazmaya çalışmak (yalnız `flag` satırı kabul eder) · çapa durumunu
sınıflandırma fonksiyonu yerine yeni bir `NOT EXISTS` ile yazmak.

## M15-16 — Domain: talep penceresi, gönderim, karar

- **Bağımlılık:** M15-15
- **Kırmızı çizgi:** §4.3 (onay = yeni satır), §4.6, §4.5, ADR 0006 (kilit), ADR 0009, ADR 0011, ADR 0025 (askı), §7
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(claim): let an employee claim the missing half and a manager decide it`

**Amaç.** Yeni paket `internal/domain/claim` (CLAUDE.md §3 dizin haritasına satır — bu commit'te):

1. **Çapa durumu — ortak yüklemle** (ADR 0032 §A.6, §C.3), başka bir tanımla değil: çıkış talebinin
   çapası *açık* bir `in` (sonraki ilk yönlü olay onu kapatan bir `out` değil), giriş talebinin çapası
   *yetim* bir `out`. Go tarafı `tap`'in sınıflandırma fonksiyonunu, sorgu tarafı SQL fonksiyonunu çağırır.
2. **Pencere** (saf; `tap`'in iş günü fonksiyonunu çağırır):
   - **çıkış talebi**: `(in.At, min(sonraki yönlü olay, çapanın iş gününün sonu, şimdi)]` — iş gününün
     sonu çapanın `business_day_start`'ından (yoksa etkin kesimden) bir sonraki iş günü başlangıcı;
   - **giriş talebi**: `[max(önceki yönlü olay, çapanın iş günü başlangıcı), out.At)`.
3. **Gönderim:** kişinin advisory kilidi (`LockEmployeeForTap`) altında çapa ve komşular okunur; çapa
   **hâlâ** eksik mi (adım 1), çalışan `active` mi, an pencerede mi → INSERT + `audit_log`
   (`claim.submitted`) tek işlemde.
4. **Karar:** müdür (`decided_by` ≠ çalışan — 00005'in öz-onay yasağının aynası). Kilit altında
   yeniden doğrulama (adım 1 ve 2); **onay** (ya da *Edit*: müdürün saati, yine pencerede) →
   `manual.Recorder`'ın yazımı **aynı işlemde** (bugün `Record` kendi `WithTenant`'ını açar — işlem
   alan bir varyanta bölünür, davranışı değişmez), mekân = çapanın mekânı (eş mekânsa
   `SiteLocationID`, `InsertManualSiteTransaction` yolu; eşlik bittiyse `ErrVenue` → müdür *Edit*'te
   mekân seçer), not *"from the employee's claim"*; karar satırı; `audit_log` (`claim.approved` /
   `claim.rejected`, `detail`: talep ve onay anı). **Ret** → yalnız karar + audit; anomali kalır.
5. **Süre dolması:** talep, `claimed_at` `manual.MaxBackdate` (90 gün) dışına çıkınca karar
   verilemez (`manual.Bounds` zaten reddeder — tek temsil); kuyrukta *"expired"*, satır yazılmaz,
   iş (job) yok.
6. **Artık gereksiz:** çapa bu arada başka yoldan kapanmışsa onay reddedilir (`ErrClaimSuperseded`),
   kuyruk *"already resolved"* der ve yalnız **Reject** sunar.
7. **Deaktive çalışan:** bekleyen talep karara açık kalır (iş deaktivasyondan önce yapıldı; kayıt
   kaybolmaz), kuyrukta işaretli; deaktive çalışan yeni talep **gönderemez**.
8. **Askı (ADR 0025) — kapalı istisna listesi genişletilmez.** Müdürün karar yolu (onay / *Edit* /
   ret) bir panel yazmasıdır ve **ana yazma grubuna** gider (`ProtectWriting`, `internal/handler/dashboard.go:166`;
   askıdaki tenant'ı reddeden halka `:144-148`'de anlatılır): askı talep kararını **durdurur**. Bu bir
   kayıt kaybı değildir: talep satırı yazılmıştır ve kalır, çapa anomali listesinde durur, ve askının
   durdurmadığı **manuel kaydın iki adımı** (`:330-331`, kardeş grup `protectWritingThroughSuspension`
   `:309`; ADR 0025 *"Tek cümlede"* `:46-53`, bağlam tablosu `:87`) telafi yolu olarak açık kalır.
   Talep onayını o sekiz rotalık kapalı listeye eklemek ADR 0025'in değişikliği olurdu; M15 bunu
   yapmaz. Çalışanın talep **gönderimi** (`POST /api/claim`, M15-17) bir tap yüzeyidir ve askıyı
   bilmez — tap gibi (ADR 0025 `:49-53`: *"tap, aktivasyon, … askıyı bilmez"*).

**Kabul kriterleri.**
- **Üçüncü göz vakası** (DB): Pzt 15:00 `in`, Sal 07:00 `out` (`employee`, S = Sal 05:00) → Pzt için
  çıkış talebi **kabul** (pencere Sal 05:00'e kadar), Sal 07:00 için giriş talebi **kabul**; Pzt'nin
  talebi onaylanınca (Pzt 23:00) ledger Pzt'yi 8 sa sayar, Sal 07:00 yetim kalır (*"giriş eksik"*).
- Pencere tablo testleri: Rusty Bar çıkış talebi 02:00'ye kadar (ertesi takvim günü) kabul; iş günü
  sonundan sonra ret; sonraki girişten sonra ret; giriş talebinde önceki iş günü reddi.
- ADR 0011 ölçümüyle: çıkış talebi onayı Q18 vakasını kapatır (0 → 8 sa, açık 1 → 0); giriş talebi
  onayı yetim çıkışı 8 sa'lik aralığa çevirir; **hiçbir onay bir aralığı uzatmaz** — `correction_test.go`
  deseninde ölçülür.
- Eşzamanlılık (`-race`): aynı talebe iki eşzamanlı onay → tam 1 karar, tam 1 manuel satır.
- Onay işlemi yarıda kalırsa (hook ile hata) manuel satır **da** karar satırı **da** yok.
- Log ve `audit_log` `detail`'inde e-posta, oturum, imzalı bağlam yok (`redline-check.sh` R7 yeşil).
- CLAUDE.md §3 dizin haritasında `internal/domain/claim/` satırı (bu commit'te).
- Askıdaki bir tenant'ta (fixture) talep gönderimi yazılır; karar yolu M15-18'in rotasında reddedilir
  (aşağıda), talep satırı ve çapanın anomalisi yerinde kalır.

**Tuzaklar.** Manuel satırı ve karar satırını ayrı işlemlerde yazmak (yarım onay) · pencereyi
yalnız gönderimde doğrulamak · çapa durumu için yüklem dışı bir tanım · talep saatini çıplak saat
olarak saklamak (an UTC'dir; duvar saati handler'da çözülür).

## M15-17 — Sonuç ekranı talep kartı ve `POST /api/claim` (§9 istisnası)

- **Bağımlılık:** M15-16
- **Kırmızı çizgi:** §9 (sonuç ekranı — kullanıcı onayı 2026-10-21), §4.5, §4.6 (talep hatası dokunuşu etkilemez), §4.2 (kart GPS okumaz), §7
- **Araç:** yapıcı, skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(tap): ask for the missing half on the result screen`

**Amaç.** Kayıt yazıldıktan sonra sonuç ekranında **isteğe bağlı** tek kart:

- **Çıkış eksik:** karar önceki iş gününün (ya da eski) açık girişini açık bıraktıysa —
  *"Yesterday you clocked in at 09:12 but there's no clock-out. What time did you leave?"* (gün
  sözcüğü tarihe göre: *Yesterday* / *On Monday*); saat seçici çapanın çözülmüş vardiya bitişiyle
  dolu, pencereye kırpılmış.
- **Giriş eksik:** çalışan `out` seçti ve kapatılabilir giriş yoktu — *"What time did you come in?"*,
  vardiya başlangıcıyla dolu.
- **Tek kart** — ikisi birden mümkünse giriş talebi (bugünkü, taze olan) gösterilir; dünkü açık
  giriş anomali listesinde kalır.
- Kart yalnız çapa için talep yoksa ve çapa `manual.MaxBackdate` içindeyse çıkar. Gönder düğmesi
  (*"Send to my manager"*); atlamak için düğme yok — *"You can skip this."* cümlesi ve bugünkü
  *"All done — you can close this page."* kalır.

**Bağlam:** imzalı talep bağlamı (yeni MAC etiketi, `tapcontext.go`'nun deseni: oturum id'si ek
doğrulanmış veri, sunucu saatiyle `IssuedAt`, TTL `tapContextTTL` ile aynı 15 dk) — içinde çapa id'si
ve tür; **istemcinin gönderdiği çapa id'sine güvenilmez**. `POST /api/claim`: aynı Origin kuralı,
`SessionLive`, bağlam doğrulaması, `<input type="time">` duvar saati → pencere içindeki tek UTC an
(pencere < 24 sa olduğu için tektir; sonbaharın iki kez yaşanan saatinde erken olan — testle
pinli), `claim.Submit`. Yanıt: *"Sent — your manager will review it."*, düğme yok. Hız sınırı tap
yüzeyinin oturum bütçesi (`internal/httpx/ratelimit.go`). **Askı:** `POST /api/claim` tap yüzeyidir,
panelin yazma zincirinde değildir ve askıyı **bilmez** — `POST /api/checkin` gibi (ADR 0025 `:49-53`);
askıdaki bir tenant'ın çalışanı talep gönderebilir, kararı askı kalkana kadar bekler (M15-16 adım 8).

**CLAUDE.md ve skill (bu commit'te, tek yerde):** §5 *"Unutulan çıkış"* maddesine talep cümlesi; §9
onay ekranı istisnası (ADR 0032 *"CLAUDE.md metin önerileri"*); skill `tappa-brand`'in sonuç ekranı
kuralı (*"Onay ekranında buton yok"* maddesi) istisnayla. Faz C kapanışı (M15-19) skill'e ayrıca
dokunmaz.

**Kabul kriterleri.**
- Kartsız sonuç ekranları **bayt-aynı** (`TestUnbrandedScreens_AreByteIdenticalToTheGolden` ve
  `TestResultScreen_DrawsTheLogoAndNoAccent` değişmeden yeşil); kartlı şekiller için yeni golden'lar.
- Kart accent taşımaz; tema `<link>`'i 0 (sonuç ekranının kuralı).
- Bağlam başka oturumdan → 400, satır yok; süresi dolmuş → *"this question has expired"* (dokunuş
  yazılmıştır); pencere dışı saat → formu yeniden gösterir, satır yok.
- `POST /api/checkin`'in hiçbir yolu talep okumasının hatasıyla değişmez (talep durumu okunamazsa
  kart çıkmaz, sonuç ekranı yine çizilir — §4.6).
- `getCurrentPosition` sonuç ekranında çağrılmaz.
- CLAUDE.md §5 *"Unutulan çıkış"* cümlesi, §9 onay ekranı istisnası ve skill `tappa-brand` sonuç
  ekranı maddesi bu commit'in diff'inde.

**Tuzaklar.** Talep bağlamını tap bağlamıyla aynı etiketle imzalamak (birinin bağlamı ötekinin
uç noktasında geçer) · saat seçiciyi tarihsiz bir duvar saati olarak UTC'ye çevirmek (gece yarısını
aşan pencere) · karta *"Skip"* düğmesi koymak (ikinci eylem; kullanıcı onayı yalnız gönderimi kapsar).

## M15-18 — Panel: talep kuyruğu

- **Bağımlılık:** M15-16
- **Kırmızı çizgi:** §4.5 (müdür yazma yolu), §4.3, §9 (panel), ADR 0025 (askı)
- **Araç:** yapıcı, skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(panel): approve or edit a punch claim in one tap`

**Amaç.** Onay kuyruğu ekranında (bugün flag'li dokunuşlar — `internal/handler/review.go`) ikinci
bölüm *"Time claims"*: satır *"Ahmet: yesterday out 23:30"* — **[Approve]** **[Edit]** **[Reject]**.
*Edit* saat alanını açar (pencere ipucuyla). Anomali ekranının *"çıkış eksik"* ve *"giriş eksik"*
satırlarında *"claim pending"* rozeti kuyruğa bağlanır. Onaylanan manuel satırın fişi notunda
talepten geldiğini söyler.

**Rota:** karar POST'ları `mountWriting`'in **ana grubuna** (`ProtectWriting()`,
`internal/handler/dashboard.go:166`) — flag kararının (`reviewHref`, `:167`) yanına; kardeş gruba
(`protectWritingThroughSuspension`, `:309`) **değil** (M15-16 adım 8). Askıdaki tenant'ta kuyruk
okunur, karar ADR 0025'in 403'ünü ve cümlesini alır.

**Kabul kriterleri.** Approve tek POST (PRG); ikinci onay *"already decided"*; başka tenant'ın talep
id'si → bulunamadı (sızma yok); süresi dolmuş ve *"already resolved"* talepte yalnız Reject; manager
ve owner ikisi de karar verebilir — bugünkü flag kuyruğuyla aynı (**ölçüldü:**
`internal/handler/review.go:359` `reviewDecision`'da rol kontrolü yok; rota `dashboard.go:167`,
`mountWriting` ana grubu, `ProtectWriting()` arkasında); askıdaki tenant'ta karar POST'u 403 ve ADR
0025'in cümlesi, kuyruk okuması açık, talep satırı değişmez; M12 `Panel*` bileşenleri; 44 px hedefler.

## M15-19 — Faz C kapanışı

- **Bağımlılık:** M15-15 … M15-18 · **Araç:** ayrı denetçi + agent `tappa-security-auditor` · **Mercek: iki**
- **Kırmızı çizgi:** hepsi (faz diff'i) — özellikle §4.5 (yeni tablolar, yeni POST), §4.3, §4.7
- **Commit:** `docs(adr): record the business day and the direction chain as built` (+ yalnız düzeltme commit'leri)

**Adımlar.** Tam `make check`, `make audit`; güvenlik merceği bütün Faz C diff'inde (yeni tablo, yeni
POST, imzalı bağlam, müdür yazma yolu, log). ADR 0032'ye *"Uygulandı"* notu (gerçekleşen ad ve
sınıflarla). Ekran görüntüsü turu (kartlı iki şekil, kartsız şekil; kuyruk ekranı).

**Kabul kriterleri.**
- Tam suit yeşil (`-race`); `make audit` exit 0; `TestEveryNamedTestExists` yeşil.
- Güvenlik merceği Faz C diff'inde ONAY; RLS izolasyon testleri iki yeni tabloda yeşil.
- Uçtan uca bir DB senaryosu: Pzt unutulan çıkış → Sal sabah dokunuşu `in` → kart → talep → müdür onayı
  → raporda Pzt 8 sa, anomali listesinde Pzt yok; aynı senaryo reddedilince Pzt listede kalır.
- ADR 0032'nin *"Uygulandı"* notu ve durum satırı güncel; skill `tappa-brand` bu kartta **değişmedi**
  (M15-12 ve M15-17'de değişti).
- Ekran görüntüsü turu: kartlı iki şekil, kartsız şekil, kuyruk; üç genişlik; yatay kaydırma yok.
- `main`'e birleştirme kullanıcının kararı.

---

## Bilinçli olarak yok (M15 kapsamı dışı)

- **Otomatik çıkış üretimi** (Q18 aynen: sistem çıkış uydurmaz; talep bir insan beyanıdır, kayıt
  müdür onayıyla yazılır).
- **Talep sonucunun çalışana bildirimi** (Q32).
- **`/v1` akışında yön kaynağı ya da onaylı talep** (Q31).
- **Bayat sayfa ile bilerek düzeltmenin ayrımı** (imzalı tahmin — ADR 0032 §B.3'ün dördüncü seçeneği).
- **Lokasyon düzeyinde kesim ya da `locations.timezone`** (Q01'in uygulanmamış override'ı ayrı iş).
- **"On the floor" / şerit ufku** (`ledger/floor.go`, `tape.go:110` okuma ufku 18 sa): Pzt 12:00'de
  açık kalan giriş Sal 05:00 dönümünden sonra da Sal 06:00'ya kadar *"zeminde"* sayılır. Sunum farkı,
  kayıt ya da bordro etkisi yok; pilottan sonra yeniden bakılır (ADR 0032 *"Ne garanti edilmiyor"*).
- **Geçmiş ters kayıtların düzeltilmesi** (CLAUDE.md §4.3) — zincir yayından sonraki ilk iş günü
  sınırında kendiliğinden düzelir; geçmiş satırlar ve onların ürettiği açık girişler olduğu gibi
  kalır, müdür gerekirse manuel kayıtla kapatır.
