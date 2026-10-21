# ADR 0032 — İş günü ve yön zinciri: açık giriş yalnız kendi iş gününde kapanır

- **Durum:** Önerildi (2026-10-21; plan 6. sürümüyle — mercek turları ve kullanıcı kararları K1–K6 işlendi)
- **Karar veren:** kullanıcı, 2026-10-21 — fazlar A/B/C, iki CLAUDE.md §9 onayı (tap ekranında
  tahmini yön + düzeltme bağlantısı; sonuç ekranında eksik yarı talep kartı) ve dört karar:
  **K1** kademe işletme → şube → departman → kişi (şube = çalışanın **profil** şubesi); **K2** geçmiş
  gün kartı yalnız *"What time did you leave?"* sorar, tek okutmanın aslında çıkış olduğu vaka
  anomali ekranında kalır; **K3** saat alanı vardiya saatiyle önceden dolu, müdüre işaretli, vardiya ±
  payı dışındaki talep tek tıkla onaylanamaz; **K4** aynı eksik yarı en çok **iki kez** sorulur; **K5** QR'da
  da tahmini yön ve düzeltme bağlantısı gösterilir; **K6** Faz A, Faz A kapanışı biter bitmez B ve C'yi
  beklemeden ayrıca canlıya çıkar. Bu
  ADR'nin kullanıcıya ayrıca sunduğu uygulama önerileri: §A.5–§A.6 (iş günü anının satırda
  dondurulması ve onu okuyan tek çıkış sınıflandırması), §B.3 (her iki düğme açık yön gönderir;
  yorumlanamayan `want` 400 değil *"istek yok"*), §C.1 (gösterim sayısının yeni durum tutmadan
  zincirden türetilmesi).
- **İlgili:** CLAUDE.md §4.2, §4.3, §4.5, §4.6, §5 (*"Yön"*, *"Unutulan çıkış"*), §6, §9 ·
  [ADR 0006](0006-debounce-iki-kosullu-zaman.md) (beyan edilen zaman, advisory kilit) ·
  [ADR 0008](0008-practice-satiri-ve-yon-zinciri.md) (yön zinciri, practice) ·
  [ADR 0009](0009-onay-karari-geri-alinamaz.md) (karar geri alınamaz) ·
  [ADR 0011](0011-duzeltme-satiri-yalnizca-kisaltir.md) (düzeltme yalnız kısaltır) ·
  [ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md) (tap/sonuç ekranı slotları) ·
  [ADR 0025](0025-tenant-askiya-alma.md) (askı) ·
  [ADR 0027](0027-holding-ve-capraz-tenant-dokunusu.md) §7 (zincir işverende, K-36) ·
  [ADR 0028](0028-dis-api-anahtar-akis-webhook.md) (punch akışı, K-6) · Q01, Q18 ·
  plan [M15](../plan/m15-is-gunu-ve-yon.md)

## Bağlam

### Ölçülen kusur

`internal/domain/tap/decide.go:533-544` `resolveDirection`:

```go
open := in.LastOpenIn
if open == nil || open.Practice {
    return TypeIn, ""
}
dir = TypeOut
if in.Now.Sub(open.OccurredAt) > StaleOpenIn {   // 18 sa
    note = staleOpenInNote
}
```

Pencere **yok**: açık bir giriş varsa — ne kadar eski olursa olsun — bu dokunuş `out`'tur. `StaleOpenIn`
yalnız not düşer. Sorgu `GetLastOpenTransaction` (`db/queries/transactions.sql:16-84`) da tarihsizdir.
M4-04 bunu bilinçli seçmişti: *"never silently in"* — unutulan çıkışı not ve anomali listesiyle
yüzeye çıkarmak için. Bedeli ölçülmemişti; kullanıcı ölçtü (2026-10-21, *"girerken out
yapabiliyor"*):

| Gün | Gerçek | Kaydedilen |
|---|---|---|
| Pzt 09:12 | giriş | `in` |
| Pzt 17:30 | çıkış — dokunmadı | — |
| Sal 09:05 | giriş | **`out`** (Pzt'yi kapatır, stale notu) |
| Sal 17:40 | çıkış | **`in`** |
| Çar 09:00 | giriş | **`out`** |

**Bir** unutulan çıkış zinciri **kalıcı olarak** ters çevirir. Rapor (`internal/domain/ledger`
`accumulate`) kaydedilmiş yönü sırayla eşlediği için ters zincir gece aralıklarını sayar: Sal 17:40 →
Çar 09:00 = 15 sa 20 dk, `closingTail` (= `StaleOpenIn`, 18 sa) altında → **ödenir**.

### Neden takvim günü bunu çözmüyordu

§5 takvim gününü bilerek reddetti: Rusty Bar 18:00–02:00 vardiyasında 02:10 çıkışı önceki akşamın
18:05 girişiyle eşleşmeli. Takvim günü filtresi bu eşleşmeyi koparır. Sorun "gün yok" değil, **günün
sınırının gece yarısında olmasıydı**.

## Karar

### Faz A — iş günü

**A.1 Kural.** Bir dokunuşun yönü şöyle hesaplanır. `at` dokunuşun anıdır (§A.4); `open` kişinin `at`
anında açık olan son girişidir (practice değil); `S(at)` `at`'in iş günü başlangıcıdır.

| Durum | Yön | Not |
|---|---|---|
| `open` yok (ya da practice) | `in` | — |
| `open.OccurredAt` > `at` (sıra dışı beyan) | `in` | — |
| `at − open.OccurredAt` > `StaleOpenIn` (18 sa) | **`in`** | *"previous check-in left open"* |
| `open.OccurredAt` < `S(at)` | **`in`** | aynı not |
| aksi hâlde | `out` | — |

Eski giriş açık kalır ve *"çıkış eksik"* anomalisidir (§A.6). Hata **tek iş gününe** hapsolur.

**A.2 İş günü ve kademe (K1).** Bir iş günü **gün dönümü saatinde** (yerel duvar saati) başlar ve bir
sonraki gün dönümünde biter; sınır anı **yeni** güne aittir. Kademe, en özel kazanır:

| Kademe | Sütun | Hangisi |
|---|---|---|
| kişi | `employees.day_cutoff` (NULL = devral) | çalışanın kendisi |
| departman | `departments.day_cutoff` (NULL = devral) | çalışanın departmanı (`employees.department_id`) |
| şube | `locations.day_cutoff` (NULL = devral) | çalışanın **profil** şubesi (`employees.location_id`) — **dokunduğu şube değil** |
| işletme | `tenants.day_cutoff time NOT NULL DEFAULT '05:00'` | tenant |

Etkin değer `coalesce(kişi, departman, şube, işletme)`, tek sorguda (`GetEmployeeForTap`). **Neden
profil şubesi:** zincir kişinindir; zincirde şube değiştiren biri (§5 *"zincirde şube değişimi
normaldir"*) her plakette farklı bir iş günü sınırı yaşasaydı, KF'de Sliema'da giren ve St Julians'ta
çıkan bir çalışanın tek vardiyası iki kesimle bölünebilirdi. **Profil şubesi yoksa** (`employees.location_id`
NULL — M11'in API ile yönetilen çalışanı, K-14; `00037:88` `DROP NOT NULL`) şube kademesi atlanır:
`coalesce(kişi, departman, işletme)`. KF'nin departmanı yoktur (`test/fixtures/seed.sql:190-191`:
*"KF models the location as the operational unit -- no departments"*) — orta kademeyi şube doldurur.
Departman bir şubeye bağlıdır (`departments.location_id`) ama çalışanın profil şubesinden farklı olabilir;
kademe ikisini **ayrı** okur (departman şubeden özeldir).

Saat dilimi **tenant'ınkidir** (`tenants.timezone`): kodda tek saat dilimi o — Q01'in `locations.timezone`
override'ı kararlaştırılmış ama **hiç uygulanmamıştır** (sütun yok, 2026-10-21 ölçüldü). Holding'de
**işverenin** kademesi ve saat dilimi uygulanır (ADR 0027 §7; K-36 iki üyenin saat dilimini eşit tutar);
profil şubesi işverenin kendi şubesidir.

Rusty Bar 18:00–02:00 vardiyasında 05:00 dönümüyle 02:10 çıkışı Pzt 05:00'te başlayan iş gününün
içindedir → Pzt 18:05 girişini kapatır. Fabrika gece vardiyası 22:00–06:00 için 05:00 vardiyanın
**içine** düşer (06:05 çıkışı yeni iş günüdür → `in`); bu departman bir override ister (ör. 14:00).
Ayar ekranı kaydetmeden önce uyarır ve bir değer önerir (§A.7).

**A.3 Saflık ve tek temsil.** Yön **hesaptır, politika değildir** (§5): `internal/policy`'e anahtar,
guardrail ya da baseline eklenmez. `tap.Decide` saf kalır. Brief *"saat dilimi/DST çözümü çağıranda"*
diyordu; ölçüm öncülü düzeltti: Decide **bugün de** saat dilimi çözüyor — geç kalma
`shiftStartInstant` içinde `time.LoadLocation` yapar ve `time/tzdata` paket içine gömülüdür
(`tzdata.go`). Bu yüzden `Input`'a önhesaplı bir UTC an değil **eşdeğeri** girer: `DayCutoff{Cutoff,
Timezone}` — Cutoff kademenin **çözülmüş** değeridir (kademe SQL'de çözülür, motor kademe bilmez).
Sınır `tap` paketinde **tek dışa açık saf fonksiyonla** hesaplanır:

- **`S(at)` — adaylardan ≤ `at` olan en geç.** `at`'in yerel tarihi `d` için üç aday kurulur: `d − 1`,
  `d`, `d + 1`; her aday önce **tarihi normalize edilerek** (`time.Date(y, m, d±1, 12, 0, 0, 0, loc)`'un
  tarihi alınır — `d − 1` ayın 0. günü olabilir) sonra o tarihte kesimin duvar saatiyle kurulur. `S` =
  `at`'ten sonra olmayan **en geç** aday. *(3. sürümün "o günün adayı, `at`'ten sonraysa bir gün geri"
  kuralı ölçümde düştü — motor merceği, `scratchpad/m15rev-engine/dst`: `America/Santiago` ve
  `America/Havana`'da gece yarısı DST'si var olmayan 00:00'ı Go'nun **geriye** normalize etmesine yol
  açıyor; örn. kesim 00:00, `at` = 2025-09-06 23:00 −04 → `S` = 09-06 00:00, `Next` = 09-06 23:00 —
  `S(S) ≠ S` 3366, `at ≥ Next` 90 ihlal.)*
- **Sonraki iş günü başlangıcı** `Next(S) = S(S + 26 sa)` — `S`'nin yerel tarihine bir gün eklemek değil.
- **Var olmayan ve iki kez yaşanan duvar saati açık kodla:** ilkbaharda var olmayan bir kesim (02:30)
  `time.Date`'in normalize ettiği ana düşer ve sonuç pinlenir; sonbaharda iki kez yaşanan bir saat için
  Go'nun seçimine güvenilmez (Go **sonraki** geçişi seçiyor) — aday kurulurken **erken** olan açıkça seçilir
  (aynı duvar saatini veren daha erken bir an denenir). **Tuzak (ölçüldü):** "erken olanı seç" karşılaştırması
  `d − 1`'in **normalize edilmemiş** tarihiyle yapılırsa Malta'da 02:00 kesiminde `S(S) ≠ S` 114 kez —
  tarih önce normalize edilir.
- **Ölçüm (2026-10-21, motor merceği, `scratchpad/m15rev-engine/dst/v4`):** bu algoritma on iki bölgede
  (Europe/Malta, Asia/Beirut, America/Asuncion, Australia/Lord_Howe, Pacific/Chatham, America/Scoresbysund,
  Antarctica/Troll, Africa/Casablanca, America/St_Johns, Asia/Tehran, America/Santiago, America/Havana),
  15 dk'lık her kesim, 2025–2027, 5 dk adımla **0 ihlal** verdi (`S(at) ≤ at < Next(S(at))`, `S` monoton,
  `S(S) = S`, gün uzunluğu 22–26 sa). Bu tarihli bir gözlemdir; kart aynı listeyle bir özellik testi yazar.

Aynı sınırı çıkış sınıflandırması (§A.6), talep penceresi (§C.3), ayar uyarısı (§A.7) ve `GET /t`
tahmini (§B.2) de ister; çağıranda hesaplanan bir an her birinde ayrı bir kopya olurdu. Kabul edilen
her IANA bölgesi (`internal/domain/tenant/account.go:557-563` `ValidTimezone` her yüklenebilir adı
kabul eder) için özellik testi — yukarıdaki on iki bölgeyle: `S(at) ≤ at < Next(S(at))`, `S` monoton,
`S(S) = S`, gün uzunluğu 22–26 sa.

`DayCutoff == nil` (kesim verilmemiş — M4 çağıranları ve mevcut testler) ya da bölge yüklenemiyor →
yalnız `StaleOpenIn` kuralı; motor 05:00 **uydurmaz**.

**A.4 Hangi an: `occurred_at`.** `at` = `tapInstant(in)`: istemci bir an beyan ettiyse `OccurredAt`,
etmediyse `Now` — geç kalmanın bugün kullandığı fonksiyon. Bugünkü `StaleOpenIn` karşılaştırması
`in.Now` ile yapılıyor; bu ADR onu da `at`'e taşır. `GetLastOpenTransaction` bir `@at` üst sınırı alır
(*"o an açık olan giriş"*; canlı dokunuşta `@at = now` → sonuç bugünküyle aynı) ve **eşzamanı `id` ile
bozar**: `(o.occurred_at, o.id) > (t.occurred_at, t.id)` ve `ORDER BY occurred_at DESC, id DESC` —
sınıflandırma ve ledger `(occurred_at, id)` sırasıyla okur (`transactions.sql:893`; `id`
`gen_random_uuid()`, `00005:49`), sorgu bugün katı `>` ve `id`'siz; aynı ana iki satırda iki okuyucu
ayrışırdı.

*Neden beyan edilen an?* M9-01'in çevrimdışı kuyruğu (todo; ama `occurred_at` form alanı
`POST /api/checkin`'de **bugün canlı**) şu şekli üretir: Pzt 17:00 çıkışı kuyruğa girdi, Sal 09:00
kişi çevrimiçi `in` yaptı, kuyruk 09:01'de Pzt 17:00'yi gönderdi. Sunucu saatine göre bu çıkış Sal'ın
iş gününe düşer ve yanlış yönlenir; beyan edilen ana göre Pzt'nin açık girişini kapatır.

*Saldırı/sapma yüzeyi:*

| Hamle | Sonuç | Sınır |
|---|---|---|
| Geleceğe beyan | `sys:occurred-at-bound` → `reject`, yön yok | guardrail |
| > 72 sa geçmiş | aynı | guardrail |
| 120 sn – 72 sa geçmiş | `base:queued-window` → **`flag`**; müdür görür | baseline (tenant ayarlayabilir) |
| ≤ 120 sn geçmiş, kesimi geçecek biçimde | yön `auto` etiketiyle değişir | yalnız kesimin ±2 dk'sında; Faz B'de yön zaten seçilebilir — kazanç yalnız *etiket* |
| Seçimi yönlendirme (ADR 0006 SELECTION) | `@at` geçmişe çekilmeli → yukarıdaki satırlar | aynı |

Yönün kendisi saat **yaratmaz** (§A.6); saat yaratan beyan edilen anın kendisidir ve o yüzey değişmedi
(ADR 0006, M5-05 K1).

**A.5 Dondurma: `transactions.business_day_start`.** Her yönlü **nfc/qr** kararının iş günü başlangıcı
(UTC) satıra yazılır; tarihsel satırlarda, manuel satırlarda ve yönsüz kararlarda NULL. Okuyucusu çıkış
sınıflandırmasıdır (§A.6). Neden dondurulur: kesim sonradan değişebilir, ve geçmişi bugünkü ayarla
yeniden hesaplayan bir okuyucu geçmiş haftanın eşleşmesini değiştirir — `00016`'nın fatura dönemlerinde
saat dilimini dondurmasının gerekçesi. Bu yüzden S **yeniden hesaplanamaz** bir kanıttır (§*"Geri alma"*).

`insertParams` yönsüz bir kararda (`Type == nil`) **ve** manuel kanalda (`checkin.Record` hâlâ
`ChannelManual` kabul ediyor — `internal/domain/checkin/checkin.go:1102`) S'yi ve (Faz B) `direction_source`'u
**kodda** NULL'a zorlar. Sütunların tutarlılık CHECK'i (`… IS NULL OR type IS NOT NULL`) ikinci
emniyettir: bir kod hatası CHECK'e çarpıp kaydı 23514 ile düşürmemeli (§4.6). CHECK **`NOT VALID`**
eklenir ve ayrı bir migration'da doğrulanır (`00044`'ün deseni; motor merceğinin ölçümü: düz `ADD
CONSTRAINT CHECK` ~2 M satırda ~195 ms `ACCESS EXCLUSIVE` altında tam tarama yapar — `ADD COLUMN` meta
veri işlemidir, CHECK değildir); her `ALTER TABLE transactions` dosyası `SET LOCAL lock_timeout = '1s'`
ile başlar (`00037:52`, `00038:39`'un kuralı). Tarihsel NULL'lar CHECK'ten geçer — `NOT VALID`'in
bilerek seçilmesinin nedeni doğruluk değil kilit süresidir.

**A.6 Tek çıkış sınıflandırması — rapor, anomali ve talep kapısı aynı tanımı kullanır.** Bugün iki tanım
var ve ikisi de Faz A'dan sonra yanlış:

- `ListOpenCheckIns` / `CountMaskedOpenCheckIns`: *"sonrasında herhangi bir `out` yok"*
  (`db/queries/anomalies.sql:701-708`). *"Pzt `in`, Sal `in`, Sal `out`"* şeklinde Sal'ın çıkışı Pzt'nin
  girişini **da** kapatır; *"çıkış eksik"* listeden silinir.
- `accumulate`: sıralı ve 18 sa'i aşan çifti açık sayar (`ledger/report.go:964-988`) ama iş gününü
  bilmez; Faz B'de çalışanın seçtiği `out` önceki iş gününün girişini kapatır (Pzt 15:00 + Sal 07:00 =
  16 sa → **ödenir**).

**Yüklem.** Bir `out`, açık bir `in`'i **kapatır** ⇔ `out.At − in.At ≤ StaleOpenIn` **ve**
(`out.S IS NULL` **veya** `in.At ≥ out.S`). Bir `in` **açıktır** ⇔ kişinin ondan sonraki **ilk** yönlü,
practice olmayan olayı (`(occurred_at, id)` sırasıyla) onu kapatan bir `out` değildir. İkinci koşul
yalnız S'si dolu satırlara uygulanır; motorun `auto` çıkışları onu zaten sağlar (motor yalnız
`open.OccurredAt ≥ S` iken `out` der).

**Çıkışın sınıfı — altı ayrık sınıf, her biri tek sayaç.** `P` = çıkıştan önceki ilk yönlü, practice
olmayan olay (`(occurred_at, id)` sırasıyla); `I` = çıkıştan **önce** (aynı `(occurred_at, id)` sırasıyla)
ve `occurred_at ≥ S` olan bir `in` var mı; `W` = `P`, çıkıştan **sonra yazılmış** mı
(`P.created_at > out.created_at`).

| Sınıf | Koşul | `in` | Rapor sayacı | *"Giriş eksik"* |
|---|---|---|---|---|
| **kapatır** | `P` bir `in`, iki koşul sağlanıyor | kapalı | süre | — |
| **(a) çok uzun** | `P` bir `in`, S koşulu sağlanıyor (ya da S NULL), 18 sa koşulu **düşüyor** | açık; çıkış onu tüketir | `TooLong` | — |
| **(b) başka iş günü** | `P` bir `in`, S dolu, `in.At < S` | açık | `OutWithoutIn` | **evet** |
| **(c) yetim** | `P` `in` değil, S dolu, (e) değil | — | `OutWithoutIn` | **evet** |
| **(e) fazla çıkış** | `P` bir `out`, S dolu, `I` **ve** `W` | — | `StartedEarlier` (bugünkü gibi) | — |
| **(d) önsüz, S NULL** | `P` `in` değil, S NULL | — | `StartedEarlier` (bugünkü gibi) | — |

- **(a) ile (b) birlikte düşerse sınıf (b)'dir** (S dolu, `in.At < S` ve > 18 sa). Değerlendirilen
  öneri (a) idi — bugünkü davranışı korur ve saat yaratmaz. Seçilmedi, çünkü **bugünkü davranış bu
  hücrede yoktur**: S'si dolu satır M15'ten önce yazılmadı, ve motor `auto` bir çıkışta S koşulunu hiç
  bozmadığı için pratikte yalnız **çalışanın seçtiği** bir çıkışta oluşur. İki sınıf da saat yaratmaz;
  (a) seçilirse müdür o çıkışın kendi iş gününün girişsiz olduğunu **görmez**. S koşulu daha özgül
  nedendir.
- **(e) neden var — yönetici düzeltmesi (motor merceği M-Y1).** Motor 09:00 `in`, motor 20:00 `out`
  (S dolu), müdür sonradan 17:00 manuel `out` yazar → sıra `in 09`, `out 17` (kapatır, 8 sa), `out 20`.
  20:00'nin `P`'si bir `out`'tur; (c) sayılsaydı ADR 0011'in *"out too late"* düzeltmesi her seferinde
  **kalıcı** bir *"giriş eksik"* bırakırdı; sınıfsız bırakılsaydı *"stranded row is counted rather than
  lost"* garantisi (`ledger/report.go:850-853`) bozulurdu. (e) bugünkü sayaçta (`StartedEarlier`) kalır;
  ADR 0011'in iki tablosu **S dolu varyantlarda da** aynı sonucu verir.
- **(e)'nin ayırt edicisi `W` — neden (5. sürüm; iki merceğin ortak YÜKSEK bulgusu).** 4. sürümde (e)
  yalnız `I` ile tanımlıydı ve **moladan sonra unutulan girişi yutuyordu** (motor merceği `sim4`): 09:00
  `in`, 12:00 `out`, 13:00'te dönüş dokunulmadı, 17:00'de motor `in` der, çalışan *"Not right? Clock
  out"* seçer → `I` var (09:00) → (e) → `StartedEarlier` 1, `OutWithoutIn` 0; 13:00–17:00 *"giriş
  eksik"*te yok, talep kartı çıkmaz. Faz B'nin en sık düzeltme şekli bu. Fark: düzeltmede `P` çıkıştan
  **sonra** yazılmıştır (müdürün satırı geçmişe eklenir), molada `P` çıkıştan **önce** yazılmıştır.
  `created_at` sunucunun yazdığı ve değişmeyen değerdir (`DEFAULT now()`, `00005:124-125`; hiçbir sorgu
  onu yazmaz — `InsertTransaction`'ın sütun listesi, ölçüldü; satır append-only). Ölçüm (`sim4`): *"out
  too late"* → `P.created` 24 > 23 → (e); mola → 26 < 27 → (c).
  - **`I` neden hâlâ gerekli — `W` tek başına Faz C'nin kendi akışında sinyali gizler.** Pzt 15:00 `in`,
    Sal 07:00 çalışanın seçtiği `out` (S Sal 05:00, (b)); müdür Pzt'nin **çıkış talebini** onaylar →
    manuel `out` Pzt 23:00, Sal 09:00'da yazılmış. Sal 07:00'nin `P`'si o manuel çıkıştır ve `W` doğrudur;
    `I` olmasaydı (e) olur ve Sal'ın girişsiz günü listeden düşerdi. Sal'ın iş gününde giriş yoktur →
    `I` yok → (c). İkisi birlikte: *"çıkışın iş gününde bir giriş var **ve** önceki çıkış sonradan
    eklenmiş"* = bir düzeltmenin fazlası.
  - **Geriye tarihli beyanla gizleme denendi — gizlenmez, yalnız yer değiştirir.** Mola vakasında
    17:00'nin (c)'sini (e)'ye çevirmek için önüne **sonradan yazılmış** bir çıkış gerekir: 17:01'de
    `occurred_at = 16:59:59` beyanlı bir `out` (≤ 120 sn, `base:queued-window` bayrak kaldırmaz; 60 sn
    debounce sunucu saatinde geçer). 17:00 (e) olur — ama 16:59:59'un kendi `P`'si 12:00 çıkışıdır ve
    ondan **önce** yazılmıştır → **(c)**, listede. Genel olarak, bir günün kapanmış çiftinden sonraki ilk
    çıkışın `P`'si o kapanış çıkışıdır ve onu (e) yapmak için kapanış çıkışının **sonradan** yazılmış
    olması gerekir — yani çalışanın kendi geçmişini değiştirmesi değil, bir düzeltme. 120 sn'den uzun
    geriye beyan `flag` olur ve müdür görür; 72 sa üstü ya da gelecek reddedilir.
  - **Eşzamanlı yazım:** `created_at` işlemin başlangıç anıdır; kişi başına advisory kilit beklerken iki
    dokunuşun yazım sırası ters dönebilir (ADR 0006 bunu ölçtü), ama aynı kişinin 60 sn içindeki ikinci
    yönlü dokunuşu `ignored`'dır — iki **yönlü** satır arasında ters dönme pratikte oluşmaz. Eşitlikte
    `W` yanlıştır → (c) (fazla bildirim yönünde). **Manuel yazım da aynı kilidi alır** (motor merceği DÜŞÜK-2:
    `manual.Recorder` bugün `LockEmployeeForTap` almıyor; müdürün düzeltmesi motor çıkışıyla yarışırsa
    `created_at` sırası ters dönüp (e) yerine (c) çıkardı). Kilit işlemin ilk ifadesidir; ardından gelen
    holding sayaç kilitleri (ADR 0027 §8, K-31) tap yolundakiyle **aynı sırada** alınır (kişi kilidi → sayaçlar),
    yani yeni bir kilitlenme sırası yoktur; talep onayı (§C.4) kilidi zaten alır — `pg_advisory_xact_lock` aynı
    oturumda yeniden girişlidir. **`created_at`'in tek yazarı veritabanıdır** (güvenlik merceği F-4): hiçbir
    sorgunun `transactions.created_at`'i yazmadığını pinleyen kaynak düzeyi bir test (`db/queries`'teki her
    `INSERT INTO transactions` sütun listesi, test dışı Go dosyaları ve migration fonksiyon gövdeleri — bugün
    ikisinde de yazar yok, ölçüldü). Bir duvar saati tetikleyicisi (`00033:293-308`'in deseni)
    değerlendirildi ve seçilmedi: test fixture'ları `created_at`'i bilerek geçmişe yazar (yaş ve debounce
    testleri, ör. `internal/domain/checkin/holding_db_test.go:516`) ve tetikleyici onları kırardı; kolon düzeyinde
    `INSERT` yetkisi de her yeni `transactions` sütununda bir GRANT bakımı ister.
  - **Holding ve manuel satırlar:** eş mekân satırı işverenin `transactions` satırıdır, aynı kural. Manuel
    satır `out` olarak S NULL'dır → (d); `P` olarak müdürün sonradan yazdığı satırdır → düzeltmenin (e)'si.
  - **M9-01 sınırı:** çevrimdışı kuyruktan **sonradan gelen** geçmiş bir çıkış da `W`'yi doğru yapar —
    09:00 `in`, 17:00 çıkış (motor, kapatır), 12:00'nin kuyruk çıkışı 18:00'de gelir → `in 09`, `out 12`
    (kapatır), `out 17` (e). Gerçek 13:00'te unutulmuş bir girişse sinyal (e)'ye gider. Kuyruk satırı
    `base:queued-window` ile `flag`'tir, müdür onu görür; M9-01 kartı bu sınırı bilir (plan M15-16).
  - **Güvenlik merceğinin önerisi (`direction_source ≠ 'employee'` şartı) seçilmedi:** `W` mola vakasını
    zaten (c) yapar ve gizleme denemesini yer değiştirmeye indirger; ek şart yalnız bir düzeltmenin fazlasını
    **yanlışlıkla** (c) yapabilir ve Faz A'nın SQL fonksiyonunu Faz B'nin sütununa bağlayıp 00065'te imzasını
    değiştirirdi (tek temsil iki migration'da değişirdi).
  - **Faz A'da motor çıkışı (b) ya da (c) olmaz:** motor yalnız `at` anında açık olan girişi kapatırken
    `out` der; yazıldığı anda `P` o giriştir (`closes`), sonradan araya giren her satır daha sonra yazılmıştır
    (`W`) ve aynı iş gününün girişi vardır (`I`) → (e). Yani Faz A'da (b)/(c) yalnız fixture'la ya da
    manuel/kuyruk satırlarıyla oluşur; M15-05'in geçici eşlemesi üretimde ulaşılmazdır (bir özellik testi
    pinler).
- **Tarihsel satırlar değişmez:** S NULL bir çıkış yalnız *kapatır*, (a) ya da (d) olabilir — tam
  bugünkü üç dal (`report.go:964-988`). Ölçülen tarihsel stale şekil (Pzt 09:00 `in`, Sal 09:05 `out`,
  S NULL, 24 sa) → (a): `TooLong` 1, `StartedEarlier` değişmez, *"giriş eksik"*te değil.
- **Faz A ara durumu:** sınıflandırma okuyuculara Faz A'nın ilk okuyucu kartında girer, `OutWithoutIn`
  sayacı ve *"giriş eksik"* listesi bir sonraki kartta. Arada (b) ve (c)'nin çıkış yarısı **geçici
  olarak `StartedEarlier`**'a yazılır (hiçbir çıkış sayaçsız kalmaz); ikinci kart onları
  `OutWithoutIn`'e taşır.
- **Dönem sınırı (yalnız rapor):** ledger dönemden önceki olayları okumaz; dönem içindeki bir **yetim
  (b ya da c)** çıkışın S'si dönem başından önceyse önündeki giriş okunmamış olabilir — rapor onu
  `StartedEarlier` dikişi sayar. Açık `in` başka bir dönemdeyse (bugünkü `continue` dalı) kapatır/(a)
  sınıfında bugünkü gibi hiçbir şey sayılmaz; (b) sınıfında çıkış kendi dönemindeyse ve S dönem
  içindeyse `OutWithoutIn` sayılır. Anomali listeleri dönem değil satır sorusu sorar.
- **Açık slot:** (b) dalında `openEntry(in)`'den sonra açık slot boşalır (`open = nil`, bugünkü iki
  dalın yaptığı gibi); aynı kişinin sonraki ikinci `out`'u önsüz olur — `I` ve `W` varsa (e), yoksa (c).
- **Yarım aralık (motor merceği DÜŞÜK-2):** `I` artık zaman aralığıyla (`[S, out.At)`) değil **sırayla**
  tanımlı — aynı `occurred_at`'te, `id`'si küçük bir `in` çıkıştan önce sayılır, büyük olan sayılmaz. `W`
  seçimi bu bulguyu düşürmez; tanımı ayrıca düzeltildi.

**İki temsil, bir parite testi.** Sınıflandırma Go'da `tap` paketinde tek saf fonksiyondur (girdi:
`P`'nin yönü ve anı ya da yokluğu, `I`, `W`, çıkışın anı ve S'si → sınıf; *"kapatır"* onun bir sınıfıdır;
ledger, şerit, talep domain'i çağırır) ve SQL'de tek `IMMUTABLE`, `SECURITY INVOKER`, tablo okumayan
bir fonksiyondur (migration 00064; `P`, `I`, `W` çağıran sorguda alt sorgularla bulunur;
`ListWorkedShiftEvents` `created_at`'i döndürür). Parite testi altı sınıfın her hücresini ve sınırları
(`in.At == S`, tam 18 sa, eşzamanlı iki satır, `W` eşitliği, mola ve düzeltme şekli) iki tarafta
değerlendirir; SQL'deki süre `tap.StaleOpenIn`'den türetilerek karşılaştırılır
(`TestGuardrails_TagNotActiveIsAnAllowList`'in şema-kod bağı deseni).

**`GetLastOpenTransaction` bu sınıflandırmayı kullanmaz — kesim sabitken gerekmez.** Kapatmayan bir
çıkışın arkasında kalan giriş, o çıkıştan sonraki her dokunuş için de önceki iş gününde ya da 18 sa'ten
eskidir; motor onu zaten kapatmaz. Bu **kesim sabitken** doğrudur: kesim iki dokunuş arasında
değişirse (05:00 → 14:00) sorgu bir girişi gizleyebilir ve motor `in` der. Sınır **muhafazakârdır**:
çıkış uydurulmaz, saat yaratılmaz, giriş *"çıkış eksik"* listesinde kalır.

Yan etkiler (doğru yönde, ekran söyler): ADR 0008'in maskelediği girişler ve tarihsel *"stale →
`out`"* satırlarının kapattığı sanılan girişler pencerede **açık** görünür; `CountMaskedOpenCheckIns`'in
*"closed_later"* kolu anlamını yitirir. Liste ile raporun *"Open"* kümesi aynı pencere için aynıdır
(`anomalies.sql:647-652`'nin *"meşru fark"* cümlesi yeniden yazılır).

**A.7 Ayar ekranı.** İşletme varsayılanı Account'ta (owner — `internal/handler/accountactions.go:96`
`mayEditAccount`), şube override'ı şube formunda, departman override'ı departman formunda, kişi
override'ı kişi kartında **yeni bir eylemde** (panelde *"çalışan formu"* yok; kişi eylemleri ayrı
rotalardır — `internal/handler/dashboard.go:168-176`); kişi kartı etkin değeri ve **kaynağını** gösterir
(*"05:00 — from Sliema"*).

**Çakışma.** Bir kesim, onu kullanan çalışanların vardiyalarından birinin **`(başlangıç − 1 sa,
bitiş + 2 sa]`** aralığına (gece vardiyasında gece yarısını aşan aralık) düşerse çakışır. *"Onu kullanan
vardiyalar"*: kesimi o kademeden devralan her çalışanın (1) çözülmüş vardiyası — departman vardiyası varsa
o, yoksa **profil şubesinin** vardiyası — **ve** (2) son **28 günde** dokunduğu şubelerin vardiyaları
(adlandırılmış sabit). (2) gerekir, çünkü kesim profil şubesinden gelir ama iş başka şubede yapılabilir:
Sliema profilli bir çalışan Rusty Bar'da 18:00–02:00 çalışıyorsa Sliema'ya 01:00 kesimi (1)'e göre
uyarısızdır ama her gecesini böler (motor merceği). **Uçlar:** sınır anı yeni iş gününe ait olduğu için alt uç
**hariç** (kesim tam `başlangıç − 1 sa` ise en erken beklenen giriş yeni güne düşer — *KM Central Kitchen*
06:00–14:00 ile 05:00 uyarı vermez), üst uç **dahil** (kesim tam `bitiş + 2 sa` ise en geç beklenen çıkış
yeni güne düşer ve `in` olur). Paylar adlandırılmış sabitlerdir (pilotta ayarlanır).

Çakışmada kayıttan önce **adlarıyla** bir uyarı, **bir öneri** ve açık onay. Örnek cümle:

> *"05:00 falls inside KM Pastry & Bakery's shift (04:00–12:00): a 03:58 clock-in would count for the
> previous day and the 12:05 clock-out would be recorded as a clock-in. Suggested: 23:00 — outside
> every shift. Changing it now would also split 3 check-ins that are open right now."*

- **Öneri:** çakışma fonksiyonu, kullanılan vardiyaların bantlarının **hiçbirine** düşmeyen en geniş
  aralığın ortasını (15 dk'ya yuvarlanmış) önerir; böyle bir aralık yoksa öneri yok ve cümle bunu söyler.
  İlk vardiya girildiğinde (şube/departman kaydı) etkin kesim yeni vardiyayla çakışıyorsa aynı uyarı ve
  öneri o formda çıkar.
- **Taşınma:** profil şubesi ya da departman değişince (`employees.sql:802-806`, panelin *"move"*
  eylemi) kişinin etkin kesimi **sessizce** değişir. Taşıma ekranı aynı çakışma uyarısını, eski ve yeni
  etkin kesimi yan yana gösterir (*"Business day starts 05:00 → 23:00 after the move"*).
- **Açık girişler:** kesim değişikliği, şu an açık olan (§A.6) ve yeni kesimle **bölünecek** girişlerin
  sayısını söyler: `B_eski = Next_eski(S_eski(in.At))`, `B_yeni = Next_yeni(S_yeni(in.At))`; giriş
  **bölünür** ⇔ `B_yeni < B_eski` **ve** `B_yeni ≤ in.At + StaleOpenIn` — yeni kesimle girişin iş günü
  eskisinden önce biter ve bu sınır girişin 18 saatinin içindedir. İkinci koşul `≤`'dir, çünkü kapatma da
  `≤`'dir: tam 18. saatteki bir çıkış bugün kapatır, `B_yeni` tam o anda ise yeni kurala göre o dokunuş yeni
  güne düşer (motor merceği N2). *(5. sürümün formülü, *"eski kesimde o aralıkta sınır yoksa"*, 05:00 → 14:00
  değişikliğinde 12:00 girişini *"bölünmez"* sayıyordu; oysa yeni sınır 14:00'tür ve 17:00 dokunuşu `in`
  olur — motor merceği.)*
- Her değişiklik `audit_log`'a.

**Ölçülen sonuç:** seed'deki KM departmanları 05:00 ile çakışıyor — *KM Pastry & Bakery* 04:00–12:00,
*KM Meat Production* 05:00–13:00 (`test/fixtures/seed.sql:195-211`). KF'nin vardiyaları (10:00–22:00,
Rusty Bar 18:00–02:00) çakışmaz. → Q30.

**A.8 Geçmiş ve yayın günü.** Geçmiş ters kayıtlar kendiliğinden düzelmez (§4.3); kural yayından
sonrasına uygulanır. Ters zincirde olan bir kişinin zinciri yayından sonraki **ilk iş günü sınırında**
düzelir; geride kalan yanlış `in` anomali listesinde durur. Yayın günü üç şey müdüre ve çalışana
söylenir: panelde bir **duyuru** (*"records before <tarih> were paired by the old rule; the open check-in
list now also shows entries the old rule had hidden"*), anomali listesinin geriye dönük değişmesi
(§A.6 yan etkileri) ve çalışanların ilk gün *"previous check-in left open"* notunu bir kez görebileceği
(deploy notu). M15 öncesi satırlar (S NULL) için talep kartı **açılmaz** (§C.1) — yalnız anomali listesi.

**Yayın anı (anahtar).** Kesimler motor onları kullanmadan **önce** ayarlanmalıdır (yoksa KM'nin fırını
ilk sabah bölünür). Tek bir yapılandırma değeri, `TAPPA_BUSINESS_DAY_FROM`, yayın anını söyler. **Etkin** ⇔
değer var **ve** sunucunun `Now`'ı ≥ değer — karşılaştırma dokunuşun **beyan edilen** anıyla değil sunucu
saatiyle yapılır (güvenlik merceği F-2: beyan edilen an istemcinindir, anahtarı geriye beyanla aşmak
mümkün olmamalı). Etkin değilken motor kesimsizdir (§A.3'ün `DayCutoff == nil` davranışı — yalnız 18 sa
kuralı, hiçbir vardiyayı kesmez), S yazılmaz ve **Faz B'nin yüzeyleri kapalıdır** (§B.2, §B.3). Kapı **tek
bir fonksiyondur** — `tap` paketinde saf (`(from, now, cutoff, tz) → *DayCutoff`), hem orkestratörün kesimi
topladığı yerde hem `GET /t` tahmininde aynı çağrı (motor merceği ORTA-2, N4).

- **Değer `checkin.Service`'e ve `tenant.Directory`'ye yapı alanı olarak enjekte edilir** (CLAUDE.md §7; paket
  düzeyi durum yok; `tenant` ile `checkin` arasında yeni import bağı yok — bugün birbirini içe aktarmıyorlar); testlerde
  harness'lar değeri **uzak geçmişe** koyar (üretim modu), *"değer yok"* ve *"`Now` değerden önce"* modları
  adlandırılmış ayrı testlerdir (motor merceği ORTA-1).
- **Biçim ve sınır** (güvenlik merceği F-3): biçim hatası başlangıçta reddedilir (`internal/config`'in kuralı —
  `TAPPA_LOG_FORMAT` gibi, `config.go:278-284`); tek pod çalışan kümede (`deploy/k8s/20-app.yaml:53`
  `replicas: 1`) bir yazım hatası bir yeniden başlatmada **hiçbir dokunuşun kaydedilmemesi** demektir. Bu
  yüzden (1) repodaki ConfigMap değerinin ayrıştırıldığını pinleyen bir paketleme testi
  (`cmd/tappa`'nın `configMapValues` yardımcısıyla `05-config.yaml`'dan okur), (2) başlangıçta *"şimdiden
  en çok 30 gün sonrası"* üst sınırı (ötesi red — `2062` gibi bir yazım hatası sessizce *"hiç etkin olmaz"*
  olmasın; geçmişteki bir değer her zaman geçerlidir), (3) değer başlangıçta loglanır (sır değildir).
- **Geri alma:** anahtarı kaldırmak yeni dokunuşları kesimsiz kurala döndürür; yazılmış (donmuş) S'ler kalır
  ve sınıflandırma onları okumaya devam eder — bu tutarlıdır (S yalnız yazıldığı dokunuş için doğrudur).

**Yayın sırası ve ilk gün (ürün merceği P1).** Faz A bu yolla **tek başına** canlıya çıkar (**K6**,
2026-10-21: B ve C'yi beklemeden — ters zincir canlıda her gün yeni değişmez yanlış kayıt üretiyor). `T0` =
sürüm (anahtar boş), `T2` = anahtar yazıldı. **`T2`, `T0` ile aynı gündür:** arada motor kesimsizdir
(18 sa kuralı) ama zaten ters olan zincirler düzelmez (dün 17:40 `in` aslında bir çıkıştı → bugün 09:00, 15 sa
20 dk < 18 sa → yine `out`), yani bekleme *"güvenli"* diye sunulmaz. Arada:
- Account, şube, departman ve kişi ekranlarında kesimin yanında bir **durum satırı**: *"Not active yet — the
  business day starts applying when Taptime switches it on"* / *"Active since <T2>"*;
- panel duyurusunun **liste cümlesi** (*"the open check-in list now also shows entries the old rule had
  hidden"*) `T0`'dan — sınıflandırma `T0`'da devreye girer —, **iş günü cümlesi** (*"From <T2>, a check-in
  is closed only within its business day; records received before that were paired by the old rule"*) yalnız
  anahtar etkinken. *"Received"* bilerek seçildi: anahtar sunucunun `Now`'ıyla karşılaştırılır (F-2), yani
  `T2`'den önce beyan edilmiş ama sonra ulaşmış bir kayıt (çevrimdışı kuyruk) yeni kuralla eşlenir;
- **çakışma bildirimi** (ürün merceği P2): canlıda dört tenant var (`state.md` OP-15 notu, *"askıda tenant
  0/4"*); pilot olmayan ikisi `T2`'de sessizce 05:00'e geçer. *"Business day check"* bir çakışma bulursa owner
  panelinin her ekranında bir `PanelNotice` (*"Your business day setting cuts 2 shifts — review it"*), `T0`'dan
  itibaren; RLS'li sıradan bir okuma, yeni `op_*` yok.
Faz B anahtar etkin olmadan sevk edilmez (§B.2); K6 ile Faz A önce çıktığı için anahtar Faz B'den önce zaten
etkin olur.

### Faz B — tahmini yön ve düzeltme

**B.1 Ekran** (kullanıcının §9 onayı, 2026-10-21). Tek birincil düğme tahmini yönü söyler: *"Clock
in"* / *"Clock out"* (bugünkü düğme *"Tap"*, sonuç *"Tapped in / out"* — Q33). Altında küçük metin
bağlantısı görünümlü ikinci gönderim: *"Not right? Clock out"* (ya da *in*); ana düğmeyle arasında en
az 24 px dikey boşluk. Tek **birincil** düğme kuralı korunur; tenant accent'i yalnız birincil düğmenin
zeminidir (ADR 0023 §2 değişmez), bağlantı accent taşımaz. Tahmin yapılamazsa bugünkü *"Tap"*
düğmesi, bağlantısız.

**B.2 Tahmin ve onun kapısı.** `GET /t` açık girişi (`@at = now`), etkin kesimi ve saat dilimini okur
ve `tap`'in dışa açık tahmin fonksiyonunu çağırır — `resolveDirection` de **aynı** fonksiyonu çağırır.
Tahmin tavsiyedir: karar `POST`'ta, advisory kilit altında verilir (ADR 0006). Eş mekân plaketinde
tahmin işverenin zinciridir.

**Kapı (güvenlik merceği G-O3, F-1; K5).** `GET /t` sayfası CMAC doğrulanmasa ve plaket aktif olmasa da
çizilir (`internal/handler/tap.go:445-480`: reddin **kaydı** POST'ta yazılsın diye). Tahmin ise kişinin
*"şu an mesaide mi"* durumunu söyler. Tahmin ve düzeltme bağlantısı yalnız şu kapıdan geçince çizilir;
aksi hâlde bugünkü yönsüz *"Tap"*:
- **anahtar etkin** (§A.8 — sunucunun `Now`'ı ≥ `TAPPA_BUSINESS_DAY_FROM`; güvenlik merceği F-1: anahtar
  yokken S yazılmaz, çalışanın seçtiği önsüz bir çıkış (d) olur, *"giriş eksik"*te görünmez ve talep kartı
  açılmaz — §B.5'in koruması S'ye dayanır), **ve**
- **NFC:** `pv.CMACValid`; **QR** (K5): `p.Channel == qr` **ve** `PreviewWithoutReplayProtection` hatasız
  (QR'da CMAC yoktur — `internal/sun/preview.go:93-95`, `:232-235`), **ve**
- `pv.TagStatus == active` **ve** çalışan `active`. Merceğin önerdiği ek şart — NFC'de salt-okunur `ctr > last_ctr` — **reddedildi**:
`sun.Preview` bilerek sayaç taşımaz (`internal/sun/preview.go:57-65`: bir önceki sürüm `last_ctr`'ı
veriyordu ve `CMACValid && ctr > LastCtr` cümlesi yazılabiliyordu — §4.4'ün adını verdiği TOCTOU; yansıma
testleri bir sayaç alanında kırmızı). Bu yüzden biriktirilmiş bir URL (A1, kabul edilmiş risk) yine
tahmin gösterir — yalnız oturumun sahibine, onun kendi durumunu (DÜŞÜK kabul edilmiş risk; güvenlik
merceği reddi kabul etti).

**QR'da tahmin var (K5, 2026-10-21 — 5. sürümün *"QR'da tahmin yok"* kararının yerine).** Kullanıcının
gerekçesi: açığa çıkan yalnız kişinin kendi durumudur (oturum çerezi gerekir), kaydedilmiş bir NFC URL'siyle
aynı risktir, ve QR kaydı IP yoksa zaten `flag`'e düşer (`base:qr-requires-ip`). **Kabul edilmiş sınırlar:**
QR fotoğraflanabilir ve süresiz geçerlidir, yani statik QR + oturum çerezi her zaman tahmini gösterir —
oturumun sahibine, kendi durumunu. **Kabul edilmiş risk (güvenlik merceği v4 O-3):** fotoğraflanmış bir QR
**ve çalınmış bir oturum çerezi** ile bir başkası çalışanın *"şu an mesaide mi"* durumunu sorgulayabilir. QR URL'si imzasızdır (`?tag=<uid>` — `internal/sun/params.go:145-146`, `:185-188`): bilinen bir uid ve çalınmış bir oturum çerezi yeter; eski bir NFC URL'sinden `ctr` ve `cmac` silinince QR olur, bu yüzden NFC'deki CMAC şartı tahmin için bir fren **değildir** — kapının fiilî hâli *anahtar etkin + plaket aktif + çalışan aktif*'tir. Risk kullanıcının K5 gerekçesiyle aynıdır: açığa çıkan yalnız oturum sahibinin kendi durumu.
Çalınmış çerez zaten çalışan adına dokunuş yapabilir (daha ağır bir yetenek); tahmin ona yeni bir yazma
yolu vermez, yalnız bir okuma — K5 bunu kabul eder.

*Gelecek seçenek (bağlayıcı değil, uygulanmaz):* sunucu içinde `EXISTS (SELECT 1 FROM tags WHERE uid =
$1 AND last_ctr < $2)` gibi tek bir boolean `TapPageFacts`'e girebilir — sayaç sayfaya ya da `sun.Preview`'a
girmez, yalnız *"bu URL henüz harcanmamış"* sorusunun cevabı. Bedeli: eş mekân plaketinde `tags` tenant'ın
RLS'i altında okunamaz, `holding_tap_site`'ın genişlemesi (ADR 0027) gerekir. Biriktirilmiş URL'nin
tahmin göstermesi bir gün sorun olursa bu yol açıktır.

**B.3 Düzeltmenin semantiği — her düğme açık bir yön gönderir.** Tahmin ile POST arasında durum
değişebilir: başka bir sekme ya da cihaz (60 sn sonrası), iş günü sınırının geçmesi, `StaleOpenIn`'in
dolması. Üç semantik değerlendirildi:

| Semantik | Bayat sayfada ana düğme | Bayat sayfada bağlantı | Seçildi mi |
|---|---|---|---|
| Ana düğme = *"motoru kabul et"* (alan yok), bağlantı = açık yön | motorun yeni `D`'si kaydedilir — kişi *"Clock out"* gördü, `in` yazıldı | açık yön | ❌ kişinin gördüğüne aykırı kayıt |
| İmzalı tahmin (`ctx`'te `Pred`) + *"çevir"* | — | bayat `Pred`'in tersi = kişinin **istemediği** yön | ❌ |
| **Her iki düğme açık yön** (`want=in|out`) | **basılan yön** kaydedilir, `D` farklıysa `employee` | basılan yön | ✅ |

Ana düğme `want=<tahmin>`, bağlantı `want=<tersi>` gönderir (ikisi de `<button type="submit"
name="want">`). Kilit altında motor `D`'yi hesaplar; kaydedilen yön `want`'tır. **Değişmez:
`direction_source = 'employee'` ⇔ kaydedilen yön ≠ kilit altında hesaplanan `D`.** `want` yoksa motor
karar verir, `auto`. İmzalı bağlama (`ctx`) bir şey girmez; `tapContextVersion` değişmez.

*Dördüncü seçenek — tahminin de gönderilmesi (`shown=<tahmin>`), seçilmedi.* İmzasız `shown`
sahtelenebilir (bilerek değiştiren `shown=want` gönderir); imzalı biçimi bağlam sürümü değişikliği ve
üçüncü bir kaynak değeri ister, kazancı nadir bir vakayı (60–180 sn bayatlık, çoğunlukla kesim anı)
ayırmaktır. Pilot gürültü gösterirse eklemeli olarak sonradan.

*`want` alanının yorumu — asla 400 değil (§4.6).* Bütün `want` değerleri toplanır, boşlar atılır; hiç
kalmazsa istek yok; hepsi aynı ve `in`/`out` ise o yön; çelişen ya da tanınmayan değer → istek yok + bir
uyarı log satırı (değerin kendisi loglanmaz). Bozuk `occurred_at`'in 400'ü (`handler/checkin.go:194-199`)
bilinçli olarak izlenmez: `want` yalnız bir tercihtir ve güvenli bir yorumu vardır — motorun kararı.

*Form ve `tap.js` (ölçüldü).* `form.submit()` submitter'ın `name`/`value`'sunu göndermez; konum
API'siz yolda betik doğal gönderime izin verir ama `markPending` düğmeyi eşzamanlı `disabled` yapar ve
devre dışı submitter değer göndermez (`web/static/js/tap.js:67-71`, `:97-103`). Kural: HTML'de statik
gizli `want` yok; `tap.js` `submit` işleyicisinin ilk işi olarak — `markPending`'den ve dal ayrımından
önce — basılan düğmenin değerini (`event.submitter`; yoksa `click` işleyicisinin sakladığı değer)
**tek** bir gizli alana yazar (varsa yeniden kullanır, yoksa oluşturur — bfcache ya da ikinci gönderim
iki alan üretmez), sonra iki düğmeyi birlikte devre dışı bırakır. Değer yakalanamazsa `want` gitmez →
motor karar verir. GPS çağrısı tek kalır (§4.2).

**B.4 Kayıt.** Verdict, güven puanı, kanıt değişmez. `transactions.direction_source text` (`auto` |
`employee`), yalnız INSERT; tarihsel satırlar NULL (okuyucu kuralı: yönlü nfc/qr satırında NULL =
*"M15 öncesi, motorun seçimi"*), manuel satırlarda NULL. İstek politika bağlamına girmez. Reject /
ignored / redirect'te istek düşer ve iki sütun NULL.

**İki dil:** kaydın notu (müdür, rapor) *"direction chosen by employee"*; çalışanın sonuç ekranındaki
not (`EmployeeNote`, ADR 0029'un ayrımı) insan dilinde *"You chose Clock out."*. Aynı ayrım açık
bırakılan giriş notunda: kayıt *"previous check-in left open (no checkout recorded)"*, ekran
*"Your previous clock-in has no clock-out."* (metinleri yapıcı ve denetçi kesinleştirir; Q33 ile
birlikte).

**B.5 Saat şişirme yolu ve sıra.** Çalışanın seçtiği bir `out`'un önceki iş gününün açık girişini
kapatması §A.6'nın sınıflandırmasıyla **Faz A'da** kapanır; yetim çıkışın sayacı ve listesi de Faz
A'dadır. Yolu açan şey arayüz değil `POST`'un `want` alanıdır; sıra: sınıflandırma → yetim çıkış →
motor → `POST` → arayüz. Koruma S'ye dayandığı için **`want` yalnız anahtar etkinken uygulanır** (§A.8, F-1):
etkin değilken gelen `want` yok sayılır (`auto`) ve Faz B anahtar canlıda ayarlı ve geçmişte olmadan sevk
edilmez.

**B.6 Dış API (G-O6).** `/v1` punch akışı canlıdır (`00048`–`00052`) ve olayın `direction`'ı
`transactions.type`'tır (K-40); çalışanın seçtiği yön dış sisteme **işaretsiz** gider. Q31(a) bu
yüzden Faz B'nin **sevk kapısıdır**: canlıda aktif API anahtarı ya da webhook'u olan bir tenant varsa
`directionSource` alanı (eklemeli, v1 ile geriye uyumlu) Faz B ile **birlikte** sevk edilir. Alan saha
tenant'ının satırına da geçer (holding punch'ının iki hedefi — ADR 0028 §4); ADR 0027 §9 madde 3
(`0027:915-918`) sahanın gördüğü sütunları katalogla pinler — o maddeye tarihli bir **K notu** ve katalog
pininin genişletilmesi kartın kabul kriteridir.

### Faz C — eksik yarı talebi

**C.1 Akış ve ne zaman sorulur.** Talep kayıt **değildir**. Sonuç ekranında, dokunuş kaydı yazıldıktan
sonra, isteğe bağlı **tek** kart (kullanıcının §9 onayı):

- *Çıkış eksik* — karar bir açık girişi açık bıraktıysa: önceki iş gününden/eski (*"Yesterday you clocked
  in at 09:12 but there's no clock-out. What time did you leave?"*) **ya da** aynı iş gününde çalışanın
  *"Not right? Clock in"* seçimiyle (bölünmüş vardiya: 11:00 `in`, 15:00 çıkışı unutuldu, 18:00'de
  çalışan `in` seçti — ürün merceği Ü-O2).
- *Giriş eksik* — çalışan `out` seçti ve çıkış yetim (b ya da c): *"What time did you come in?"*.
- İkisi birden mümkünse giriş talebi (bugünkü, taze olan); öteki anomali listesinde kalır. Aynı dokunuşta
  iki **çıkış** çapası yarışırsa (dünden açık A ve bugün çalışanın *"Not right? Clock in"* seçimiyle açık
  bıraktığı B) en taze çapa (B) gösterilir; A anomali listesinde.
- Kart yalnız sonucu **`ok` ya da `flag`** olan (yönlü) bir dokunuşta çıkar; `ignored` ve `reject`
  sonucunda kart yoktur ve bu dokunuşlar gösterim fırsatı sayılmaz (yönsüzdürler). `flag`'te kapanış
  cümlesi kartın altında kalır.
- **Yalnız M15 sonrası çapalar:** çapanın `business_day_start`'ı **dolu** olmalı (ürün merceği Ü-Y2). Yayın
  günü ters zincirden kalan eski girişler için *"What time did you leave?"* yanlış sorudur — onlar yalnız
  anomali listesinde.
- **En çok iki kez (K4) — tür başına.**
  - **Çıkış talebi** (çapa açık bir `in`, A): (1) A'yı açık bırakan dokunuş — A'dan sonraki ilk yönlü olay
    `T1`; (2) atlanırsa `T1`'in iş gününde `T1`'den sonraki **ilk `out`**.
  - **Giriş talebi** (çapa yetim bir `out`, X): (1) X'in kendi sonuç ekranı; (2) atlanırsa X'in iş gününde
    X'ten sonraki **ilk `out`**.
  - *(Ürün merceği P4:)* molalı bir vardiyada ikinci gösterimin *"ilk `out`"*u öğle çıkışıdır — kart öğlen
    sorar. Bilerek kabul edildi: çalışanın aklı o gün o anda en tazedir, ve kart atlanabilir.
  - *(3. sürümün tek formülü — "çapayı üreten dokunuş ya da çapadan sonraki ilk dokunuş" — giriş talebinde
    X'i, X'ten sonraki ilk dokunuşu ve onun gününün çıkışını sayıp **üç** gösterime izin veriyordu; motor
    merceği.)*
  - Sonra yalnız anomali ekranı. **Durum tutulmaz:** iki dokunuş değişmez `transactions` zincirinden
    türetilir, yani gösterim sayacı için yeni tablo, UPDATE ya da tap yolunda ek yazım yoktur (§4.3).
    Bedeli bilerek kabul edilir: kartın çizilemediği bir dokunuş (okuma zaman aşımı) da bir fırsat sayılır —
    hata **daha az** sormaya doğrudur. Append-only bir *"gösterim"* tablosu reddedildi: her sonuç ekranına
    bir yazım ekler. **M9-01 sınırı:** çevrimdışı kuyruktan sonradan gelen geçmiş bir satır *"çapadan
    sonraki ilk olay"*ı değiştirebilir ve bir gösterimi kaydırabilir (sınır yine iki fırsattır, ama hangi
    iki dokunuş olduğu değişebilir) — M9-01 kartına not.
- Kart atlanabilir (*"You can skip this."*); atlama düğmesi yok.

**K2 — kabul edilen sınır.** Geçmiş gün kartı yalnız *"What time did you leave?"* sorar. Gerçekte
**girişin unutulduğu** gün (sabah dokunmadı, akşam tek dokunuş `in` yazıldı) kart yanlış soruyu sorar;
çalışan atlar ya da yanlış bir çıkış saati talep eder, ve o tek satır müdürün anomali ekranında *"çıkış
eksik"* olarak kalır. Kullanıcı kararı (2026-10-21): *"yönü de yanlış seçebiliyorlar, plana göre
ilerlemek daha iyi"* — iki yönlü bir soru eklenmez. Müdür kuyrukta talebi çapanın yönü ve anıyla görür
(§C.4) ve reddeder ya da manuel kayıtla düzeltir.

**C.2 Veri.** İki append-only tablo, ikisi de §6'nın beşlisiyle (tenant_id, ENABLE + FORCE RLS,
`NULLIF` politikası, tenant_id önde indeks, GRANT SELECT/INSERT, REVOKE UPDATE/DELETE,
`tappa_forbid_mutation` satır tetikleyicisi **ve** `*_no_truncate` deyim tetikleyicisi — `00021:96-116`'nın
deseni; `internal/db/appendonly_truncate_test.go:88-95` `truncateTargets`'a eklenir):

- `punch_claims` — çalışan, **çapa** (`anchor_transaction_id`: açık `in` ya da yetim `out`, bileşik FK),
  `kind` (eksik yarı: `in` | `out`), `claimed_at`, **`prefilled_at`**, `created_at`; `UNIQUE (tenant_id,
  anchor_transaction_id)`. **`prefilled_at`** çapanın çözülmüş vardiyasının **gerçek** ucudur (çıkış talebinde
  bitiş, giriş talebinde başlangıç) — **yalnız o uç pencerenin içindeyse**. Uç pencerenin dışındaysa ya da
  vardiya yoksa `prefilled_at` NULL'dır, alan **boş ve zorunludur** (vardiyasız çalışanın kuralı). Pencereye
  **kırpılmış** bir ön dolgu yoktur (ürün ve güvenlik mercekleri: KF 10–22, 11:00 `in`, 15:00 unutuldu,
  18:00'de *"Not right? Clock in"* → pencere `(11:00, 18:00)`; 22:00'yi 17:59'a kırpmak *"default not
  changed"* + bant içi → tek tıkla ~7 sa, gerçek 4 sa). *"Varsayılan değiştirilmedi"* bir sütun değil bir
  **hesaptır** (`prefilled_at IS NOT NULL AND claimed_at = prefilled_at`) ve yalnız gerçek vardiya saati için
  anlamlıdır.
- `punch_claim_decisions` — `claim_id` (UNIQUE), `outcome` (`approved` | `rejected`), `decided_by`,
  `approved_at` (*Edit* ile farklı olabilir), `transaction_id` (onayın yazdığı manuel satır), `note`.

Tek tabloda `status` UPDATE'i değil, ayrı karar tablosu: ADR 0009'un deseni. `transaction_reviews`'in
kendisi kullanılamaz — tetikleyicisi yalnız `verdict='flag'` satırına karar kabul eder (`00005:347-352`).

**C.3 Kısıtlar ve pencere.**
- **Oturuma ve tek kayda bağlı:** kart, yeni bir MAC etiketiyle imzalı bir *talep bağlamı* taşır
  (`tapcontext.go` deseni: oturum id'si ek doğrulanmış veri, sunucu saatiyle `IssuedAt`, 15 dk) — içinde
  çapa id'si, tür ve `prefilled_at`. İstemcinin gönderdiği çapa id'sine güvenilmez; INSERT çapa
  satırından, `tenant_id` + `employee_id` yüklemiyle kurulur.
- **Çapa hâlâ eksik mi — §A.6'nın sınıflandırmasıyla:** çıkış talebinin çapası *açık* bir `in`, giriş
  talebinin çapası *yetim* (b/c) bir `out`.
- **Pencere — uçlar olaylarda açık, 18 sa ile kırpılmış** (güvenlik G-O4, motor M-O1):
  - çıkış talebi: `claimed ∈ (in.At, min(next, Next(in.S), in.At + StaleOpenIn, now))` — `next`
    çapadan sonraki ilk yönlü olay (**hariç**), `Next(in.S)` bir sonraki iş günü başlangıcı (**hariç**:
    sınır anı yeni güne ait), `in.At + 18 sa` **dahil** (kapatma koşulu `≤`), `now` dahil;
  - giriş talebi: `claimed ∈ (max(prev, out.S, out.At − StaleOpenIn), out.At)` — `prev` (**hariç**),
    `out.S` **dahil** (`in.At ≥ S`), `out.At − 18 sa` dahil, `out.At` hariç.
  Pencere ≤ 18 sa'tir; bir duvar saati yine de sonbaharın iki kez yaşanan saatinde **iki** ana karşılık
  gelebilir (25 sa'lik gün) — handler pencere içindeki adaylardan **erken** olanı açık kodla seçer.
- **Onayda yeniden doğrulama `tap`'in sınıflandırmasıyla:** onaylanacak manuel satırın kişinin zincirine
  eklendiği hâli sınıflandırılır; yeni çift **gerçekten** `closes` olmalı (çıkış talebinde yeni manuel
  `out`'un sınıfı; giriş talebinde çapa `out`'un yeni `in`'le sınıfı). Değilse onay reddedilir
  (*"already resolved"*). Bu, pencere aritmetiğindeki bir hatanın saate dönüşmesini ikinci kez engeller.
- **Gönderim ve karar advisory kilit altında** (`LockEmployeeForTap`).
- **Log:** talep yalnız opak id'lerle loglanır. `audit_log` satırı `claim.submitted`'da `ActorID` nil'dir
  (çalışan kendisi adına — `internal/audit/audit.go:59-61`), kararda müdürün id'si.

**C.4 Karar — müdürün ekranı (K3, G-Y1, Ü-O4).** Talepler bugünkü onay kuyruğunun **odak desenini**
izler (`web/templates/pages/review.templ:12-24`: kuyruk bağlantı listesidir, karar formu odaktaki tek
kayıt içindir, iki sonuç formun iki gönderim düğmesidir, betik yok, kısayol yok). *"Tek tık"* = odak +
**Approve**; *Edit* odak formunun içinde bir `<details>` (saat alanı). **Toplu onay yoktur** —
`internal/handler/review.go:49-57`'nin kararı aynı gerekçeyle (her karar ayrı bir kişinin ücreti
hakkında ayrı bir hukuki işlemdir). Odaktaki talep müdüre şunları gösterir:
- çapa: an, yön, kaynağı (`direction_source = employee` ise işaretli), kanal;
- komşu olaylar (çapadan önceki ve sonraki yönlü olay);
- **oluşacak aralık ve süresi** (*"in 05:00 → out 09:00 = 4 h 00 m"*);
- çözülmüş vardiya ve sapma (*"shift 09:00–17:00 · 4 h before the shift starts"*);
- *"default not changed"* işareti (yalnız `prefilled_at` doluysa ve `claimed_at = prefilled_at`).

**Tek tık yalnız bandın içinde:** talep edilen an vardiyanın §A.7 bandının (`(başlangıç − 1 sa,
bitiş + 2 sa]`) **dışındaysa** ya da çalışanın çözülmüş vardiyası **yoksa**, Approve doğrudan yazmaz —
odak formu bir onay adımı ister (aralığı ve sapmayı yineleyen ikinci gönderim). Güvenlik merceğinin
vakası (G-Y1): Ahmet 09:00'da *"Not right? Clock out"* → yetim (c); giriş talebi 05:00 → +4 sa; ya da
çıkış talebi Sal 03:00 → 17 sa 48 dk. İkisi de bandın dışında: kuyrukta tek tıkla onaylanamaz ve müdür
aralığı, süreyi ve sapmayı görmeden onaylayamaz.

**Müdürün *Edit*'i de aynı banda tabidir** (bilerek): *Edit* ile girilen an bant dışındaysa aynı onay
adımı istenir. Müdür kasıtlı bir karar veriyor ama risk (bir aralığın şişmesi) aynıdır ve iki yolun farklı
kurala tabi olması *"Edit'le gir, adımı atla"* kısayolunu açardı.

**Onay** (ya da *Edit*) tek işlemde: `channel='manual'` satır (`internal/domain/manual`'ın yazımı — işlem
alan bir varyanta bölünür; `entered_by` = onaylayan, mekân = çapanın mekânı), karar satırı, `audit_log`.

**Öz-onay — dürüst sınır (G-O1).** `00005:354`'ün kuralı `reviewer_id`'yi (bir `admin_users` id'si)
çalışanın `employees` id'siyle karşılaştırır; iki ayrı kimlik uzayı olduğu için **bugün hiçbir zaman
tutmaz** (`internal/handler/review.go:470-473`: *"it is unreachable today"*). Talep kararı aynı
karşılaştırmayı yaparsa aynı boşluğu taşır. Bu ADR **bir öz-onay güvencesi vaat etmez**: aynı kişi hem
yönetici hem çalışan kimliği taşıyorsa kendi talebini onaylayabilir. Kapanması M14'ün kimlik birleşmesine
(ADR 0031, M14 dalı) bağlıdır; o zamana kadar her karar `audit_log`'da kimin verdiğiyle görünür.

**ADR 0011 ile çatışma yok:** çıkış talebi Q18'in kendi vakasıdır (*"kapanmamış in 09:00 + müdür out
17:00 → 8 sa, açık 0"*). Giriş talebi yetim bir çıkışın önüne bir `in` koyar; ADR 0011'in tuzağı ancak
arada yanlış bir `in` varsa kurulur ve pencere (*"önceki yönlü olaydan sonra"*) bunu dışarıda bırakır.

**Askı (ADR 0025).** Karar yolu bir panel yazmasıdır ve `mountWriting`'in **ana grubuna**
(`ProtectWriting`, `internal/handler/dashboard.go:166`; flag kararı `:167`'nin yanı) gider: askı talep
kararını durdurur. ADR 0025'in kapalı istisna listesi (`0025:46-53`, `:87`; kardeş grup
`dashboard.go:309`, manuel kayıt `:330-331`) **genişletilmez** — telafi yolu zaten oradadır. Çalışanın
talep gönderimi (`POST /api/claim`) bir tap yüzeyidir (`Tap.Mount`, `internal/handler/tap.go:270-276`,
`chain()`) ve askıyı bilmez (`0025:49-53`).

**C.5 Ret, süre, deaktivasyon.**
- **Ret:** karar + audit; anomali kalır, müdür yine manuel kayıt girebilir. Aynı çapaya ikinci talep yok.
- **Süre dolması:** `claimed_at` `manual.MaxBackdate` (90 gün) dışına çıkınca karar verilemez —
  manuel yazım zaten reddeder; kuyruk *"expired"* der. Satır yazılmaz, iş (job) yok.
- **Artık gereksiz:** çapa başka yoldan kapandıysa onay reddedilir (*"already resolved"*), yalnız Reject.
- **Deaktive çalışan:** bekleyen talep karara açık kalır; deaktive çalışan yeni talep gönderemez.
- **Çift gönderim:** aynı çapaya ikinci *"Send"* unique ihlali (23505) alır ve **"Sent" ekranına**
  eşlenir — 500 değil.

## Geri alma (Down) — kanıt silinmez

`business_day_start` yeniden hesaplanamaz (§A.5), `direction_source` hiç hesaplanamaz, talepler ve
kararları birer beyan ve karar kaydıdır. Bu migration'ların `Down`'ı, düşüreceği sütunda NULL olmayan bir
değer ya da tabloda bir satır varsa **reddeder** (`RAISE EXCEPTION … USING ERRCODE =
'object_not_in_prerequisite_state'`) — emsal `00043_record_a_tap_on_a_peers_plaque.sql:430-433`. Kesim
ayar sütunları yapılandırmadır, kanıt değildir; geçmişi `audit_log`'dadır. **İlk dokunuştan sonra geri
dönüş yolu şemayı değil binary'yi geri almaktır:** eski kod yeni sütunları yazmaz ve okumaz; CHECK'ler
NULL'a izin verir.

## Değerlendirilen alternatifler

| Alternatif | Neden reddedildi |
|---|---|
| **Takvim günü** (gece yarısı sınırı) | Rusty Bar 02:10 çıkışını önceki akşamın girişinden koparır. Gün dönümü takvim gününün **sınırı kaydırılmış** hâlidir. |
| **Sabit 18 sa pencere** (`StaleOpenIn`'i tek kural yapmak) | Kullanıcının **tam** vakasını çözer (23 sa 53 dk), ama vardiyalar arası boşluk 18 sa'ten kısaysa çözmez: geç vardiya Pzt 16:00 girişi, unutulan çıkış, Sal 09:00 erken vardiya → 17 sa → yine `out`. 18 sa iş gününün **ikinci** şartı olarak kalır. |
| **Vardiyaya göre en yakın eşleşme** | Vardiyası olmayan çalışanda karar verilemez; esnek ve çift vardiyada en yakın uç sık yanlış; vardiya değişince geçmişin anlamı değişir. Vardiya yalnız **uyarıda** kullanılır. |
| **Şube kesimini dokunulan şubeden almak** | Zincir kişinindir; şube değiştiren biri tek vardiyasında iki sınır yaşar (K1). |
| **Yönü politika yapmak** | §5: yön hesaptır. Tenant'ın değiştirebildiği şey bir **parametredir** (kesim). |
| **Decide'a önhesaplı UTC iş günü anı** | Geçerli ve saf; ama sınırı dört okuyucu daha ister — dört kopya. |
| **Sunucu saatine (`Now`) göre iş günü** | Çevrimdışı kuyruğun dünkü çıkışını bugüne düşürür (§A.4). |
| **İş gününü raporda bugünkü ayardan yeniden hesaplamak** | Kesim değişince geçmiş haftanın eşleşmesi değişir (`00016`'nın gerekçesi). |
| **Tarihsel satırlara `direction_source='auto'` doldurmak** | Yönsüz satırlarla çelişir, değişmez satırlara iddia ekler. |
| **Düzeltmeyi `flag`'e düşürmek** | Kullanıcı kararı. Saat şişirme §A.6'nın sınıflandırmasıyla ve §C.4'ün bandıyla kapanır. |
| **Fazla çıkışı (e) yetim saymak** | Her ADR 0011 *"out too late"* düzeltmesi kalıcı bir *"giriş eksik"* bırakırdı (§A.6). |
| **(e)'yi yalnız `I` ile tanımlamak** (4. sürüm) | Moladan sonra unutulan girişi yutar (§A.6, `sim4`). |
| **(e)'yi yalnız `W` ile tanımlamak** | Faz C'nin kendi akışında (önceki günün çıkış talebinin onayı) bugünün girişsiz gününü gizler (§A.6). |
| **(e)'ye `direction_source ≠ 'employee'` şartı** | `W` mola vakasını zaten çözer; ek şart düzeltmenin fazlasını yanlış (c) yapabilir ve Faz A'nın SQL fonksiyonunu Faz B'nin sütununa bağlardı (§A.6). |
| **Ön dolguyu pencereye kırpmak** | Kırpılmış bir *"vardiya saati"* gerçek değildir ve tek tıkla şişirme yolu açar (§C.2). |
| **Global anahtar yerine `tenants.day_cutoff` NULL ⇒ o tenant kesimsiz** (motor merceği DÜŞÜK-4) | Daha basit: owner kesimi kaydedince devreye girer, global anahtar ve yayın sırası gerekmez. Seçilmedi: K1 *"varsayılan 05:00"* kararına aykırıdır (owner'ı kaydetmeye bırakır; kaydetmeyen tenant hiç iş günü almaz ve ters zincir orada sürer) ve kademe her seviyede bir *"kesimsiz"* anlamı taşımak zorunda kalırdı. Global anahtar kalır. |
| **QR'da tahmin göstermemek** (5. sürüm) | K5 ile değişti (§B.2). |
| **Çakışma raporu için RLS'i atlayan bir araç ya da yeni `op_*`** | §4.5: tenant sınırını aşmanın tek yolu denetim izli `op_*`'tır ve tek seferlik bir rapor bir ADR 0021 genişlemesine değmez; uyarı zaten tenant başına (§A.7). |
| **Tahmin için salt-okunur sayaç kontrolü** | `sun.Preview`'un bilerek kaldırdığı TOCTOU malzemesi (§B.2). |
| **Gösterim sayısı için bir tablo** | Her sonuç ekranına bir yazım ekler (§C.1). |
| **İki yönlü geçmiş gün sorusu** (*"giriş mi çıkış mı unuttun?"*) | Kullanıcı kararı K2. |
| **Talepleri `transaction_reviews`'e yazmak** | Tetikleyici yalnız `flag` satırı kabul eder. |
| **Onaylı talebi doğrudan kayıt saymak** | Q18: çalışanın kendi beyanı kayıt değildir. |
| **Toplu onay** | `review.go:49-57`'nin gerekçesi; G-Y1. |

## Sonuçlar

**Olumlu.** Unutulan çıkış tek iş gününe hapsolur; ters zincir yayından sonraki ilk sınırda düzelir;
gece vardiyası aynen çalışır; anomali listesi, rapor ve talep kapısı aynı tanımı kullanır; çalışan
yanlış tahmini dokunduğu anda düzeltir; eksik yarı taze bellekle ve müdürün odaklı tek onayıyla kapanır.

**Bedeller.**
- Kesim ayarı yanlışsa Faz A o departmanda bugünkünden **kötü**dür. Uyarı, öneri ve canlı çakışma
  raporu bunu yayından önce yakalamak içindir (Q30).
- Göreli saatle kurulmuş DB testleri kesim sınırını duvar saatine göre geçebilir (motor merceği M-Y2);
  test kesimini duvar saatinden uzağa koyan bir yardımcı ve iki sabit saatle ölçüm gerekir.
- Tap ekranının golden'ları bilinçli olarak yeniden üretilir; sonuç ekranının kartsız golden'ları
  bayt-aynı kalır.
- `transactions`'a iki sütun, `locations`/`departments`/`employees`/`tenants`'a birer kesim sütunu, iki
  yeni tablo, iki yeni POST yüzeyi (talep gönderimi, kişi kesimi) ve bir talep karar yolu.
- `CountMaskedOpenCheckIns`'in anlamı değişir; anomali ekranındaki sayılar ilk yüklemede artar.

## Kırmızı çizgi analizi

| Çizgi | Etki |
|---|---|
| **§4.1 biyometri** | Yok. |
| **§4.2 GPS** | **Değişmez.** `GET /t` tahmini ve sonuç ekranındaki talep kartı konum okumaz. Düzeltme bağlantısı aynı formun ikinci gönderimidir; `tap.js`'in **tek** `getCurrentPosition` çağrısından geçer. |
| **§4.3 immutable** | `transactions`'ta UPDATE/DELETE yok; yeni sütunlar yalnız INSERT'te yazılır, tarihsel satırlar NULL kalır. Onay = yeni manuel satır + `audit_log`. `punch_claims` ve kararları append-only (REVOKE + satır ve TRUNCATE tetikleyicileri). Gösterim sayısı için durum tutulmaz (§C.1). `Down` dolu bir kanıt sütununu ya da talep satırını düşürmeyi reddeder. |
| **§4.4 atomik ctr** | Dokunulmaz. Tahmin kapısı sayaç **okumaz** (§B.2). |
| **§4.5 tenant izolasyonu** | Canlı çakışma raporu tenant başına, **owner'ın kendi bağlamında** okunur (panelde salt okunur bir bölüm); RLS'i atlayan bir araç repoya **eklenmez** ve yeni bir `op_*` açılmaz. İki yeni tablo §6 beşlisiyle; bütün yeni sorgular açık `tenant_id` filtreli; talep INSERT'i çapa satırından tenant + çalışan yüklemiyle; talep bağlamı oturuma bağlı MAC. Holding'de zincir, kademe ve talep işverenindir. `tenants.day_cutoff` için kolon düzeyinde GRANT. Öz-onay yasağı bugün boştur — dürüst sınır (§C.4). |
| **§4.6 kayıt kaybolmaz** | Yön kuralı hiçbir dokunuşu düşürmez. Yeni kısıtlar sözlük ve tutarlılık kontrolüdür ve ikinci emniyettir. Yorumlanamayan `want` 400 değil *"istek yok"*. Talep kartının okuması kayıt işleminin **dışında**, commit'ten sonra, sınırlı bir sürede; hatası yalnız kartı düşürür (sonuç ekranı yine çizilir). Hiçbir çıkış sayaçsız kalmaz (§A.6, altı sınıf). Sessiz onay yok. |
| **§4.7 sırlar** | Yeni sır yok; talep bağlamı sır içermez; oturum token'ı ve e-posta loglanmaz. |

## CLAUDE.md metin önerileri

CLAUDE.md bu ADR ile **değişmez**; her paragraf onu uygulayan kodla **aynı commit'te** değişir.

**§5 — *"Yön (in/out)"* maddesinin yerine** (ilk paragraf Faz A motor kartı; ikinci paragraf Faz B
motor kartı):

> - **Yön (in/out):** kişinin **son açık girişi** (dokunuşun anında açık olan), bu dokunuşun **iş günü
>   başlangıcından önce değilse** ve `StaleOpenIn`'den (18 sa) yeniyse bu dokunuş onu kapatır (`out`);
>   değilse bu dokunuş `in`'dir ve eski giriş *"çıkış eksik"* anomalisi olarak kalır (notla) — hata
>   tek iş gününe hapsolur. **İş günü takvim günü DEĞİLDİR:** gün dönümü saatinde (yerel duvar saati,
>   varsayılan 05:00; kademe işletme → şube (çalışanın **profil** şubesi) → departman → kişi, en özel
>   kazanır) başlar; tenant saat diliminde ve dokunuşun **beyan edilen** anında hesaplanır (ADR 0032).
>   Rusty Bar 18:00–02:00 vardiyasında 05:00 dönümüyle 02:00 çıkışı akşamın girişini kapatır; dönümü
>   kesen bir gece vardiyası (fabrika 22:00–06:00) departman override'ı ister — ayar ekranı uyarır.
>   Yön **hesaptır**, politika değildir.
>   Çalışan tap ekranında tahmini yönü değiştirebilir: kayıt istediği yöndür, verdict değişmez,
>   `direction_source='employee'` + not. Rapor bir çıkışı yalnız kendi iş gününün ve 18 saatin içindeki
>   girişle eşler; iş günü anı olmayan satırlar (manuel ve M15 öncesi) yalnız 18 sa kuralıyla eşlenir.

**§5 — *"Unutulan çıkış"* maddesine eklenecek cümle** (Faz C talep kartı):

> Çalışan eksik yarıyı sonuç ekranından **talep** edebilir (`punch_claims`; en çok iki kez sorulur);
> talep kayıt değildir, müdürün onayı `channel='manual'` kayıt + `audit_log` yazar.

**§9 — *"Çalışan tap ekranı kutsaldır"* paragrafına** (Faz B arayüz kartı ilk cümle, Faz C talep kartı
ikinci):

> Sayılı istisnalar (kullanıcı §9 kararı, 2026-10-21 — ADR 0032): tek birincil düğme tahmini yönü
> söyler (*"Clock in"* / *"Clock out"*) ve altında küçük bir *"Not right? …"* bağlantısı vardır;
> onay ekranında, yalnız eksik bir yarı varsa, tek bir talep kartı ve onun *"Send to my manager"*
> düğmesi vardır. Başka her değişiklik yine önce sorulur.

**§3 — dizin haritasına** (Faz C domain kartı): `internal/domain/claim/` — *eksik yarı talebi: pencere,
gönderim, müdür kararı (onay → manuel kayıt). ADR 0032.*

**Ayrıca güncellenecekler (Faz B arayüz kartı):** skill `tappa-brand` `SKILL.md:484` (*"kelimesi 'Tap'
… tek yeni metin logonun alt'ıdır"*), ADR 0023 §6 madde 10 (`0023:861`, *"tap ekranında yeni metin"*)
— tarihli not, ve `web/templates/pages/tap.templ:52-53`'ün yorumu (*"one button, its word 'Tap'"*).

## Bulunan riskler (brief'te yoktu)

1. **İki ayrı "açık" tanımı** (§A.6) — tek sınıflandırma, iki temsil, parite testi.
2. **KM kesim çakışması** (§A.7, Q30).
3. **`tap.js` ve form** (§B.3).
4. **Override ile saat şişirme** (§A.6, §B.5) ve **talep onayıyla saat şişirme** (§C.4, G-Y1).
5. **API akışı** (§B.6, Q31) — Faz B'nin sevk kapısı.
6. **Eski davranışı pinleyen testler** — yeni 18 sa kuralı kesim **yokken de** geçerlidir:
   `decide_test.go:597-634`, `crosslocation_test.go:197-237`, `internal/handler/checkin_db_test.go:1808-1830`
   ters iddiayla yeniden yazılır. Sabit not metnini görünüm girdisi olarak kullanan testler
   (`result_test.go:1018-1032`, `unbranded_golden_test.go:152`, `ledger/partnernote_test.go:36`,
   `ledger/holding_db_test.go:825-832`) değişmez.
7. **Saate bağlı DB testleri** (motor merceği M-Y2): 05:00 varsayılanı, göreli saatle (`now − 3 sa` gibi)
   kurulmuş testleri günün belirli saatlerinde kırmızıya çevirir — ölçülen üç:
   `internal/handler/practice_direction_db_test.go:113` (Malta 05–08 arası), aynı dosya `:163`/`:181`
   (07–10), `internal/domain/checkin/holding_db_test.go:530-553` (05:00–05:05).
8. **Yönetici düzeltmesinin bıraktığı fazla çıkış** (§A.6 (e), M-Y1).
9. **Düzeltme debounce'a takılır** — sonuç ekranında yanlış yönü görüp 60 sn içinde yeniden dokunan
   `ignored` olur; düzeltme **basmadan önce** tap ekranındadır.
10. **ADR numarası** — bu ADR ilk taslakta 0031'di; M14'ün dalı (`origin/docs/m14-identity-plan`,
    kabul `573731d`) 0031'i taşıyor (ölçüldü). M14 migration numarası ayırmıyor (`000NN`).
11. **`Down` kanıtı silerdi** (§*"Geri alma"*).
12. **Öz-onay boşluğu** (§C.4) — vaat edilmez, M14'e bağlı.

## Ne garanti edilmiyor

- **"On the floor" ve saatlik şeridin okuma ufku** (`ledger/floor.go:122`, `tape.go:110`) 18 sa kalır
  (şeridin **eşlemesi** sınıflandırmaya geçer): Pzt 12:00'de açık kalan giriş Sal 05:00 dönümünden
  sonra Sal 06:00'ya kadar *"zeminde"* sayılır. Sunum farkıdır.
- **Kesimin ±2 dk'sında** (`base:queued-window` altında) beyan edilen an yönü `auto` etiketiyle
  değiştirebilir (§A.4).
- **Kesim değişirken** motor muhafazakâr davranır (§A.6): bir giriş kapanmaz, `in` yazılır.
- **Yanlış ayar** uyarıyla onaylanırsa motor ona uyar.
- **Tek okutmanın aslında çıkış olduğu gün** (K2) anomali ekranında kalır.
- **Biriktirilmiş bir URL ve statik QR** tahmini gösterir (§B.2, K5) — oturumun sahibine; çalınmış çerezle
  bir başkasına da (kabul edilmiş risk).
- **Kesim çakışma uyarısı** profil vardiyasını ve son 28 günde dokunulan şubeleri kapsar; o pencerede hiç
  çalışılmamış bir şubeye ilk kez gidilmesi uyarıyı önceden tetiklemez.
- **M9-01'in sonradan gelen geçmiş çıkışı** bir düzeltme gibi okunur (§A.6, (e)) ve bir gösterimi
  kaydırabilir (§C.1); kuyruk satırı `flag`'tir.
- **Öz-onay** (§C.4) bugün engellenmez.
- **Talep sonucu çalışana bildirilmez** (Q32) — çalışan sonucu M9-05'e kadar göremez.
- **Geçmiş** düzelmez (§A.8).
