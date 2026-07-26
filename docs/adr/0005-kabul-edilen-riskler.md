# ADR 0005 — Kabul edilen riskler ve tespit sinyalleri

- **Durum:** kabul edildi
- **Tarih:** 2026-07-26

## Bağlam

Tappa'nın güvenlik mimarisi her tap'te **dört kanıt** toplar
([handoff.md](../handoff.md) §6): SUN = *şu an fiziksel dokunuş* · oturum çerezi =
*kim* · statik IP = *nerede* · GPS = *yedek nerede*. Bu dört kanıtın üstünde,
[ADR 0004](0004-policy-motoru-modeli.md)'te karara bağlanan **policy motoru**
durur: gevşetilemez guardrail'ler ([M3-05](../plan/m3-policy-motoru.md)) ve
yönetilen baseline ([M3-06](../plan/m3-policy-motoru.md)).

Bu mimari güçlüdür ama **her tehdidi çözmez** — ve çözemediği bazı tehditlerin
tek bilinen çözümü, ürünün **varlık sebebini** ihlal eder. En keskin örnek
[CLAUDE.md](../../CLAUDE.md) §4.1'dir: biyometrik veri **asla** toplanmaz. Parmak
izi tarayıcısı bir tek şeyi gerçekten çözer — *fiziksel bedenin belirli bir kişi
olduğunu*, yani kimliğin devredilemezliğini. Onu reddettiğimiz an, buddy
punching'i (birinin başkası adına basması) kriptografik olarak **çözemez hâle
geliriz**. Bu ADR o bilinçli tercihin **pozitif ifadesidir**: "şu riskleri kabul
ediyoruz, çünkü çözümleri kırmızı çizgiyi deler." §4.1 negatif bir yasaktır; bu
belge o yasağın bedelini yazılı, dürüst ve satılabilir hâle getirir.

**Neden yazılı.** Yazılı olmayan kabul, unutulmuş bir açıktır. İki ek sebep:
(1) müşteri ilk demoda mutlaka "peki telefonunu arkadaşına verirse?" diye
soracak — cevabımızın hazır, dürüst ve [handoff.md](../handoff.md) §2 ile tutarlı
olması gerekir; (2) "çözemiyoruz" ile "görmezden geliyoruz" **aynı şey değildir**.
Kriptografik çözümü olmayan her riski, en azından **gözlemlenebilir bir sinyale**
ve o sinyali yüzeye çıkaran **somut bir göreve** bağlıyoruz. Bu ADR'de hiçbir
risk "ileride bakarız" ile geçiştirilmez: her risk ya gerekçesiyle **kabul**
edilir, ya somut bir tespit görevine **bağlanır** — çoğu zaman ikisi birden.

**Bu ADR'nin sınırı.** Burada kabul edilenler, dört kanıtın ve policy motorunun
**mimari olarak çözemediği** risklerdir. Bir §4 kırmızı çizgisinin motor içinde
zaten *uygulanan* korumaları (atomik `ctr`, RLS, immutability, guardrail'ler) bu
belgenin konusu değildir — onlar çözülmüş tehditlerdir. Burada yalnız **kalan**
tehditler ve onların kalıntı riski durur.

**Append kuralı.** İleride yeni bir kabul edilen risk tespit edilirse, o risk
**bu ADR'ye eklenir** (aşağıdaki tabloya + kendi alt bölümüne); yeni bir ADR
açılmaz. Sebep: kabul edilen risklerin tek ve tam bir listesi olmalı ki denetim,
satış ve gelecekteki tasarım tek yere baksın. Kabul edilmiş bir risk çözülürse
(ör. bir gün cihaz-siz bir attestation yolu mümkün olursa), ilgili satır burada
**iptal edilir ama silinmez** — delil zinciri gibi, geçmiş kararın izi kalır.

## Karar

Aşağıdaki **altı risk** kabul edilir. Her biri için: neden çözemiyoruz, hangi
gözlemlenebilir sinyalle tespit ederiz, o sinyalin hangi görevde uygulandığı ve
(buddy punching için ayrıca) satış cevabı.

| # | Risk | Neden çözemiyoruz (hangi kırmızı çizgi) | Tespit sinyali | Uygulandığı görev |
|---|---|---|---|---|
| 1 | **Buddy punching** (A4/Q19) | Tek çözümü biyometri (§4.1) **veya** uygulama kurulumu — ikisi de ürünün varlık sebebine aykırı | Eş-zamanlı tap çiftleri raporu | [M6-11](../plan/m6-dashboard.md) |
| 2 | **Sahte GPS** (A3) | Web'de konum doğrulaması yok; attestation **uygulama** ister (app-less vaadi) | GPS-only tap oranı, çalışan kırılımında | [M6-11](../plan/m6-dashboard.md) |
| 3 | **URL biriktirme** (A1/Q21) | SUN payload'ında **zaman yok**; uçak modunda dokunup URL toplamak mümkün, sunucu o okumaları hiç görmez | `tap:ctrGap` → `base:ctr-gap-review`; boşluk metriği | baseline [M3-06](../plan/m3-policy-motoru.md) · metrik [M6-11](../plan/m6-dashboard.md) |
| 4 | **Mekânda bırakılmış proxy** (Y-E) | IP taşınabilir; mekânın WiFi'ında proxy+VPN, uzaktaki tap'i mekânın IP'sinden gösterir. Kriptografik çözümü yok | `tap:gpsConflict` → `base:gps-conflict-review`; "IP eşleşti ama GPS uyuşmuyor" metriği | baseline [M3-06](../plan/m3-policy-motoru.md) · metrik [M6-11](../plan/m6-dashboard.md) |
| 5 | **Müdürün kimlik basması** (Y-D) | Davet kodunu üreten ve gören kişi = bordroyu şişirmede en güçlü teşviği olan kişi | Tek cihaz/oturumdan aktive N çalışan + hiç çapraz-lokasyon göstermeyen çalışan | rapor [M6-11](../plan/m6-dashboard.md) · davet kanalı [M5-02](../plan/m5-tap-akisi.md) (Q02) |
| 6 | **Fiziksel plaket devri** | Plaket duvardan sökülüp taşınabilir; pasif çipin "yerini" doğrulayan bir bağı yok | Lokasyon–IP/GPS uyumsuzluğu → `flag`; müdür `lost` işaretler | baseline [M3-06](../plan/m3-policy-motoru.md) · guardrail [M3-05](../plan/m3-policy-motoru.md) |

Referanslanan `sid`'ler kodda gerçektir ve bu ADR'yle **birebir** eşleşir:
`base:ctr-gap-review` ve `base:gps-conflict-review` `internal/policy/baseline.go`'da
([M3-06](../plan/m3-policy-motoru.md)); `sys:tag-not-active` ve `sys:tenant-mismatch`
`internal/policy/guardrails.go`'da ([M3-05](../plan/m3-policy-motoru.md)); ikisinin
bağlam anahtarları (`tap:ctrGap`, `tap:gpsConflict`) [ADR 0004](0004-policy-motoru-modeli.md)
§8'de tanımlıdır.

### 1. Buddy punching (A4/Q19) — §4.1'in doğrudan bedeli

**Neden çözemiyoruz.** Buddy punching, bir çalışanın gerçekten orada olan
**bedeni** ile kimliği arasındaki bağın koparılmasıdır: Maria telefonunu
Giovanni'ye verir, Giovanni Maria adına basar. Bunu kriptografik olarak
kapatmanın bilinen tek yolu, her tap'te *o bedenin o kişi olduğunu* ölçmektir —
yani **biyometri** (parmak izi, yüz). Bu doğrudan [CLAUDE.md](../../CLAUDE.md)
§4.1'i ihlal eder: biyometrik veri asla toplanmaz/saklanmaz. İkinci teorik yol,
cihaz-bağlı bir **uygulama + attestation**'dır; o da ürünün "No app. No device"
([handoff.md](../handoff.md) §1) vaadini ve app-less akışını
([handoff.md](../handoff.md) §3) yıkar. İki çözüm de ürünün varlık sebebine
aykırı olduğu için buddy punching'i **kabul ediyoruz**.

**Bu, parmak izinin çözdüğü TEK şeydir — açıkça kabul.** [handoff.md](../handoff.md)
§2, rakip modelin (ZKTeco BioTime + parmak izi) acı noktalarını sayar: cihaz
başında enrollment, cihaz arızasında kör lokasyon, ıslak/yağlı/unlu elde çalışmayan
sensör, biyometrik verinin GDPR yükü, donanım+bakım maliyeti. Parmak izi
tarayıcısının bu listeye karşılık müşteriye verdiği **tek** gerçek üstünlük buddy
punching'e dirençtir — devredilemez kimlik. Tappa bu tek üstünlükten bilinçli
olarak vazgeçer ve karşılığında yukarıdaki beş yükün tamamını ortadan kaldırır.
Dürüst konumlanma budur: parmak izi bir tek şeyi bizden iyi yapar, gerisini daha
kötü.

**Tespit sinyali.** Gönüllü devri *önleyemeyiz* ama *görünür* kılabiliriz:
**eş-zamanlı (ya da fiziksel olarak imkânsız derecede yakın) tap çiftleri**. Aynı
kişinin kısa aralıkla iki farklı lokasyonda görünmesi, ya da bir cihaz oturumunun
kalıbının aniden değişmesi, gönüllü iş birliğinin istatistiksel izidir. Bu bir
*ret* sebebi değildir (masum açıklaması olabilir); müdürün gözden geçirmesi için
bir **rapordur**.

**Uygulandığı görev.** [M6-11](../plan/m6-dashboard.md) (anomali ve kötüye kullanım
raporu) — eş-zamanlı tap çiftleri kesiti.

**Satış cevabı (çekirdek).** Müşteri "peki telefonunu arkadaşına verirse?"
dediğinde cevap dört katmanlıdır ve hiçbiri gizlenmez:
- **1 oturum = 1 çalışan.** Bir telefon iki kişiyi basamaz; devir için telefonun
  **fiziksel olarak** el değiştirmesi gerekir — bir kartı gizlice uzatmaktan
  gözle görülür biçimde daha zordur.
- **Plaket kamera görüş alanındadır** ([handoff.md](../handoff.md) §5, KF St
  Julians referans kurulumu). Telefon devri kameranın önünde olur.
- **Telefonun kendi ekran kilidi** gönülsüz devri (çalınan/kapılan telefon)
  pratikte engeller — ve bu bizim katmanımız değildir, ondan **veri almayız**
  (§4.1): bedava bir caydırıcıdır, sakladığımız bir biyometrik değil.
- **Eş-zamanlı tap çiftleri raporlanır** (yukarıdaki sinyal): sistematik iş
  birliği zamanla istatistikte belirir.

**Dürüst kalan taraf.** Bunların **hiçbiri gönüllü iş birliğini durdurmaz** —
Maria gerçekten isterse telefonunu Giovanni'ye verebilir. Bunu saklamıyoruz.
Karşılığında müşteri şunlardan kurtulur: her yeni çalışan için enrollment, cihaz
arızasıyla kör kalan lokasyon, mutfakta çalışmayan ıslak-el sensörü ve biyometrik
veri saklamanın GDPR yükü. Bu, parmak izinin tek üstünlüğü (buddy punching
direnci) ile beş operasyonel/yasal yükün **bilinçli takasıdır**.

### 2. Sahte GPS (A3)

**Neden çözemiyoruz.** GPS, dört kanıttan **en zayıfıdır**: yalnızca IP eşleşmesi
olmadığında devreye giren *yedek* konum kanıtıdır ([handoff.md](../handoff.md)
§6). Tarayıcının verdiği konum, işletim sistemi düzeyinde bir "mock location" ile
sahtelenebilir; web katmanında bunu ayırt etmenin bir yolu yoktur. Gerçek bir
konum *attestation*'ı (cihazın konumunun kurcalanmadığına dair imzalı kanıt)
yalnızca kurulu bir **uygulama** + platform API'siyle mümkündür — bu da app-less
vaadini ([handoff.md](../handoff.md) §3) ihlal eder. Bu yüzden sahte GPS'i
**kabul ediyoruz** ve GPS'e hak ettiği ağırlığı veririz: güven puanında 30
(IP'nin 50'sine karşı, [handoff.md](../handoff.md) §6) ve QR kanalında **tek
başına yetmez** (`base:qr-requires-ip`).

**Tespit sinyali.** Sahte GPS ancak IP **yokken** işe yarar (IP eşleşiyorsa
zaten "binada" kanıtı vardır). Dolayısıyla gözlemlenebilir iz, bir çalışanın
**GPS-only tap oranıdır**: taplarının anormal yüksek bir kısmı yalnız GPS ile
doğrulanıyorsa (hiç IP eşleşmesi yoksa), bu ya meşru bir saha koşuludur ya da
konum sahtelemesidir — çalışan kırılımında bakıldığında ayrışır.

**Uygulandığı görev.** [M6-11](../plan/m6-dashboard.md) — GPS-only tap oranı,
çalışan kırılımında.

> **Uyarı — bu sinyal Risk 4'ü kaçırır.** GPS-only oranı yalnızca *IP'siz*
> saldırıyı yakalar. Mekânda bırakılmış proxy (Risk 4) tersine IP eşleşmesi
> **üretir**, dolayısıyla bu metrikte **görünmez** — o risk kendi sinyalini
> (`tap:gpsConflict`) gerektirir. İkisi farklı tehditlerdir; tek metrik ikisini
> birden ölçemez.

### 3. URL biriktirme (A1/Q21)

**Neden çözemiyoruz.** SUN payload'ı ([ADR 0003](0003-sdm-modu-ve-anahtar-yonetimi.md))
`tag`, `ctr` ve `cmac` taşır — **zaman taşımaz**. Bir saldırgan telefonunu uçak
moduna alıp plakete arka arkaya 10 kez dokunursa, çip her dokunuşta `ctr`'yi
artırıp geçerli birer CMAC üretir; telefon 10 farklı, imzalı, geçerli URL
biriktirir ve sunucu bu okumaların hiçbirini görmez. Sonra bu URL'ler günler
boyunca birer birer "tap" olarak gönderilebilir. Payload'da zaman olmadığı için
sunucu, gönderim anıyla dokunma anını ayıramaz. (Tap tazelik penceresi ve
`occurred_at` sınırı — [M5-10](../plan/m5-tap-akisi.md), `sys:tap-freshness`,
`sys:occurred-at-bound` — sunucunun *gördüğü* sayfa/zaman sinyaline dayanır;
biriktirilmiş URL bu pencereyi zaten geçmiş olabilir ama saldırının **özü**
sunucunun okumaları hiç görmemesidir.) Bu saldırı yalnızca **düşük trafikli
plakette** işe yarar: yoğun bir plakette (KF St Julians) biriktirilen URL'ler,
araya giren başka birinin gerçek tap'i `ctr`'yi ilerlettiği an geçersizleşir.

**Tespit sinyali.** Biriktirmenin **tek gözlemlenebilir izi** sayaç boşluğudur:
`tap:ctrGap = ctr − last_ctr − 1` ([ADR 0004](0004-policy-motoru-modeli.md) §8).
Sıfırdan büyükse, gördüğümüz son tap'ten bu yana çipe kaç kez dokunulup
**sunucuya gönderilmediğini** söyler. Baseline `base:ctr-gap-review`
(`internal/policy/baseline.go`, `SidCtrGapReview`) bu anahtara bağlanır:
`tap:ctrGap > 0` → `review`. Kaynak-kapsamlıdır (`location/*`), böylece yoğun
şubede tenant kapatabilir, düşük trafikli yerde (Rusty Bar, HQ, hafta sonu) açık
kalır — çünkü saldırı zaten yalnız orada işe yarar (Q21).

**Uygulandığı görev.** Baseline ifadesi [M3-06](../plan/m3-policy-motoru.md)'da
(`base:ctr-gap-review`, gerçek `sid`); boşluk metriği/raporu
[M6-11](../plan/m6-dashboard.md)'de.

### 4. Mekânda bırakılmış proxy (Y-E)

**Neden çözemiyoruz.** IP, "binada" kanıtının temelidir ama **taşınabilir** bir
kanıttır. Bir saldırgan, mekânın WiFi'ına bağlı prizde duran 20 €'luk bir telefonu
proxy + VPN olarak kurarsa, evden gelen bir tap'in kaynak IP'si mekânın statik
IP'si olarak görünür — IP eşleşir, "proof of place" **yanlış** olarak sağlanır.
Bunun kriptografik bir çözümü yoktur: IP bir konum kanıtı olarak zaten zayıftır,
onu güçlendiren tek şey (SUN'ın *fiziksel dokunuş* kanıtı) burada devrede
değildir çünkü saldırgan gerçek bir plakete dokunmuyor, sadece IP'yi taşıyor.
Bu yüzden **kabul ediyoruz**.

**Tespit sinyali.** Bu saldırının ayırt edici izi, IP ile GPS'in **çeliştiği**
andır: `tap:gpsConflict = true` — GPS okundu **ama** lokasyondan uzak
([ADR 0004](0004-policy-motoru-modeli.md) §8). `gpsMatch=false` bunu "GPS hiç yok"
ile aynı kefeye koyuyordu; ayrı bir anahtar gerekti çünkü proxy tap'inde IP
eşleşir ama GPS uzaktadır. Baseline `base:gps-conflict-review`
(`internal/policy/baseline.go`, `SidGPSConflictReview`): `tap:gpsConflict = true`
→ `review`.

**Uygulandığı görev.** Baseline ifadesi [M3-06](../plan/m3-policy-motoru.md)'da
(`base:gps-conflict-review`, gerçek `sid`); "IP eşleşti ama GPS uyuşmuyor"
metriği [M6-11](../plan/m6-dashboard.md)'de.

> **Uyarı — bu saldırı en tehlikeli yanılsamayı üretir.** Proxy tap'i GPS-only
> oranında (Risk 2) **görünmez**; tersine, IP eşleştiği için kayıtları "IP ile
> doğrulanmış" olarak sistemin **en güvenilir** kategorisinde gösterir. GPS
> okunmadığı durumda `tap:gpsConflict` de tetiklenmez — bu saldırının en sinsi
> biçimi, çalışanın GPS iznini hiç vermediği tap'tir. Bu yüzden `tap:gpsConflict`
> ayrı bir baseline ifadesi olarak vardır ve bu risk kendi metriğiyle
> ([M6-11](../plan/m6-dashboard.md)) izlenir; GPS-only metriğine güvenmek onu
> gizler.

### 5. Müdürün kimlik basması (Y-D)

**Neden çözemiyoruz.** Bu bir teşvik uyumsuzluğudur, bir kripto açığı değildir:
davet kodunu **üreten ve gören** kişi ile bordroyu şişirmekte en güçlü teşviğe
sahip kişi bugün **aynıdır**. Müdür sahte bir çalışan profili açar → davet kodunu
okur → kendi telefonunun ikinci tarayıcı profilinde aktive eder → o hayali çalışan
her gün trust 100 ile "orada"dır ve gerçek bir çalışandan **ayırt edilemez**,
çünkü tüm kanıtlar (SUN, oturum, IP, GPS) meşrudur. Dört kanıt "bu tap gerçek bir
plakete, gerçek bir oturumdan, gerçek bir yerden geldi" der — ama "bu oturumun
arkasında gerçek bir insan var" diyemez. Bunu tap katmanında çözmek yine
biyometriye (§4.1) götürür; kabul ediyoruz.

**Tespit sinyali.** İki gözlemlenebilir kalıp: (1) **tek cihaz/oturum
kaynağından aktive edilmiş N çalışan** — aynı cihazın aynı gün birçok profili
aktive etmesi normal onboarding'de nadirdir; (2) **hiç çapraz-lokasyon
göstermeyen çalışan** — gerçek zincir personeli şubeler arası hareket eder
([handoff.md](../handoff.md) §5), hayali bir çalışan hep tek noktada kalır.
İkisi birlikte güçlü bir sinyaldir.

**Uygulandığı görev.** Rapor [M6-11](../plan/m6-dashboard.md)'de (yukarıdaki iki
kesit). Ayrıca **teşvik uyumsuzluğunun kökü** [M5-02](../plan/m5-tap-akisi.md)'de
(davet ve aktivasyon akışı) ele alınır: Q02 çözülünce davet kodu müdürün gördüğü
bir kanaldan değil, **çalışanın kendi kanalına** (kişisel e-posta/SMS) gider —
böylece kodu üreten ile gören ayrışır ve saldırının önkoşulu (müdürün kodu
görmesi) kalkar. Bu, kabulü zamanla bir **azaltıma** çeviren somut görevdir; risk
o zamana dek kabul, sinyali M6-11'de canlıdır.

### 6. Fiziksel plaket devri

**Neden çözemiyoruz.** Plaket, tanımı gereği **pasif** bir çiptir (pil yok,
yazılım yok, internet yok — [handoff.md](../handoff.md) §2); duvara yapıştırılır
ama çipin "hangi duvarda olduğunu" kendi başına doğrulayan bir bağı yoktur.
Birileri plaketi söküp başka bir yere taşırsa, çip hâlâ geçerli SUN üretir —
`ctr` ve CMAC doğrudur, çünkü kripto çipe bağlıdır, **konuma** değil. Konum bağı
yalnızca dışarıdan gelir: çipin DB'deki `location_id`'si ile tap'in IP/GPS'i.
Çipin fiziksel yerini kriptografik olarak sabitleyemeyiz; kabul ediyoruz.

**Tespit sinyali.** Plaket taşındığında konum bağı **çelişir**: taşınan plaketten
gelen tap'in kaynak IP'si artık kayıtlı lokasyonun statik IP listesinde değildir
ve GPS de kayıtlı koordinattan uzaktır. IP de GPS de lokasyonu doğrulayamayınca
tap `base:no-evidence-review` ile **`flag`**'lenir (kanıt yok → review), taşınan
plaket GPS üretiyorsa `base:gps-conflict-review` de tetiklenir. Müdür anomaliyi
görüp plaketi panelden **`lost`** işaretler; bundan sonra o plakete gelen her tap
guardrail `sys:tag-not-active` ile **reddedilir** ([M3-05](../plan/m3-policy-motoru.md),
`internal/policy/guardrails.go`).

**Uygulandığı görev.** Kanıt-yok/çelişki baseline ifadeleri
[M3-06](../plan/m3-policy-motoru.md)'da (`base:no-evidence-review`,
`base:gps-conflict-review`); `lost` işaretli plaketin reddi guardrail
[M3-05](../plan/m3-policy-motoru.md)'te (`sys:tag-not-active`); lokasyon–IP/GPS
uyumsuzluğu kesiti [M6-11](../plan/m6-dashboard.md)'de.

## Gerekçe

- **Neden bir §4.1 "pozitif" ADR'si.** §4.1 negatif bir yasaktır ("biyometri
  yok"). Bir yasak, bedelini yazmadığı sürece bir gün "ama şu özellik için sadece
  biraz…" diye aşınır. Bedeli — buddy punching'i kabul etmek — açıkça yazmak,
  yasağı **korur**: bir daha "biyometri ekleyelim mi" tartışması çıktığında, cevap
  "hangi riski kabul ettiğimiz belli, o riski kabul etmeye devam ediyoruz"
  olur. ADR 0004 guardrail'lerin *neyi çözdüğünü* karara bağlar; bu ADR onların
  *neyi çözmediğini* karara bağlar — ikisi birlikte tam resmi verir.

- **Neden "kabul" ile "görmezden gelmek" ayrı.** Çözemediğimiz bir riski kabul
  etmek, onu izlemeyi bırakmak değildir. Her riske bir **gözlemlenebilir sinyal**
  ve o sinyali yüzeye çıkaran bir **görev** bağlamak, "çözülemez"i "görünür"e
  çevirir. Görünür bir risk, müdürün kararına açık bir risktir; görünmez bir risk
  sessiz bir kayıptır (§4.6'nın ruhu: sessiz onay da, sessiz kayıp da yasak).

- **Neden sinyaller `review` üretir, `deny` değil.** Bu risklerin hiçbiri
  kesinlik taşımaz — her birinin masum bir açıklaması olabilir (saha çalışanının
  gerçek GPS-only tap'i, meşru çevrimdışı kuyruk, gerçek şube değişimi). §4.6
  gereği kanıt belirsizken kayıt **atılmaz**, `flag`'lenir ve insana götürülür.
  Bu yüzden tespit sinyalleri baseline `review` ifadeleridir (guardrail `deny`
  değil) ve raporlardır — otomatik ret değil, müdür kararı.

- **Neden dürüst satış cevabı bir güvenlik kararıdır.** Buddy punching'i
  "çözdük" diye satmak, ilk kötüye kullanımda güveni yok eder ve rakibe
  ("aslında çözmüyorlar") koz verir. Sınırı açıkça söylemek — parmak izi bir tek
  şeyi bizden iyi yapar — hem [handoff.md](../handoff.md) §2 konumlanmasıyla
  tutarlıdır hem de beklentiyi doğru kurar: Tappa "giriş-çıkış gerçeğinin tek
  doğru kaynağı"dır ([handoff.md](../handoff.md) §3), kusursuz bir dolandırıcılık
  kalkanı değil.

## Sonuçlar

- **[M3-06](../plan/m3-policy-motoru.md) (baseline) doğrulanır:** `base:ctr-gap-review`
  (Risk 3) ve `base:gps-conflict-review` (Risk 4) bu ADR'nin kabul ettiği iki
  riskin doğrudan policy-katmanı sinyalidir; ikisi de `internal/policy/baseline.go`'da
  canlıdır. Baseline'ın bu iki ifadesi silinirse iki risk **sinyalsiz** kalır.
- **[M3-05](../plan/m3-policy-motoru.md) (guardrail) ile bağ:** `sys:tag-not-active`
  fiziksel plaket devrinin (Risk 6) son savunmasıdır — müdür `lost` işaretledikten
  sonra taşınan plaketi terminal olarak reddeder.
- **[M6-11](../plan/m6-dashboard.md) bu ADR'nin ana çıktısıdır:** altı riskin
  beşinin tespit sinyali (buddy çiftleri, GPS-only oranı, ctr-boşluk metriği,
  gps-conflict metriği, tek-cihaz/çapraz-lokasyon-yok kalıbı) bu raporda yüzeye
  çıkar. M6-11 kartı A1·A3·A4·Y-D·Y-E sinyallerini üretmekle yükümlüdür; bu ADR o
  yükümlülüğün gerekçesidir.
- **[M5-02](../plan/m5-tap-akisi.md) müdür-kimlik riskini azaltır:** Q02 çözülünce
  davet kodu çalışanın kendi kanalına gider; Risk 5'in önkoşulu (müdürün kodu
  görmesi) kalkar. Risk kabul olarak durur ama azaltım yolu bu görevdedir.
- **§4.1 sınırı normatif olarak çizildi:** parmak izinin çözdüğü tek şey buddy
  punching'dir; başka hiçbir risk için "biyometri çözerdi" argümanı bu belgede
  geçerli değildir (sahte GPS ve müdür-kimlik biyometriyle *kısmen* ilgilidir ama
  asıl çözümleri de app/attestation veya süreç ayrımıdır, biyometri tek yol
  değildir).
- **Append kuralı bağlayıcıdır:** yeni bir kabul edilen risk çıkarsa bu ADR'ye
  eklenir (tablo + alt bölüm); yeni ADR açılmaz. Bir risk çözülürse satırı iptal
  edilir ama silinmez.
- **[CLAUDE.md](../../CLAUDE.md) §4 ile tutarlı:** bu ADR hiçbir kırmızı çizgiyi
  gevşetmez; tam tersine §4.1'in bedelini kabul ederek onu korur. Kabul edilen
  riskler §4'ün *dışındaki* kalıntı tehditlerdir, içindeki korumalar değil.

## Değerlendirilen alternatifler

Bunlar, yukarıdaki riskleri "çözmek" için düşünülen ama her biri bir kırmızı
çizgiyi ihlal ettiği için **reddedilen** azaltımlardır. Reddedilmeleri, risklerin
neden kabul edildiğinin diğer yüzüdür.

| Alternatif | Hangi riski çözerdi | Neden reddedildi |
|---|---|---|
| **Biyometri** (parmak izi/yüz her tap'te) | Buddy punching (1), kısmen müdür-kimlik (5) | [CLAUDE.md](../../CLAUDE.md) §4.1 doğrudan yasak — ürünün varlık sebebi. Ayrıca [handoff.md](../handoff.md) §2'nin çözdüğü GDPR/enrollment/ıslak-el yüklerini geri getirir. Reddedildiği için (1) ve (5) kabul edilir. |
| **Zorunlu uygulama + cihaz attestation** | Sahte GPS (2), URL biriktirme (3), kısmen buddy (1) | "No app. No device" ([handoff.md](../handoff.md) §1) vaadini ve app-less akışı (§3) yıkar; rakip Connecteam'in tam da bu yükünden kaçıyoruz ([handoff.md](../handoff.md) §3). Reddedildiği için (2) ve (3) kabul edilir, `ctrGap`/`gpsConflict` sinyalleriyle izlenir. |
| **Sürekli GPS / geofence izleme** | Sahte GPS (2), fiziksel plaket devri (6) | [CLAUDE.md](../../CLAUDE.md) §4.2: GPS yalnız tap anında okunur; arka plan konum, sürekli takip, geofence izleme yok — hem GDPR gereği hem satış argümanı. Reddedildiği için (2) ve (6) tap-anı sinyalleriyle (`gpsConflict`, konum uyumsuzluğu) izlenir. |
| **Cihazlı okuyucu/kiosk** (kartı cihaza okut) | Buddy punching (1), fiziksel plaket devri (6) | Rakip Jibble'ın ters modeli ([handoff.md](../handoff.md) §3): sahada aktif bileşen = arıza noktası, bakım maliyeti, "cihazsız" vaadinin sonu. Tappa'nın tüm farkı pasif plakettir. Reddedildiği için (1) ve (6) kabul edilir. |
| **Riskleri hiç yazmamak** ("çözülemez, geç") | — | Yazılı olmayan kabul unutulmuş açıktır ve ilk demoda hazırlıksız yakalanırız. Bu ADR'nin kendisi bu alternatifin reddidir: her risk ya kabul (gerekçeli) ya somut göreve bağlı, "ileride bakarız" yok. |
