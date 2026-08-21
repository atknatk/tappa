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

Aşağıdaki **sekiz risk** kabul edilir. Her biri için: neden çözemiyoruz, hangi
gözlemlenebilir sinyalle tespit ederiz, o sinyalin hangi görevde uygulandığı ve
(buddy punching için ayrıca) satış cevabı.

> ⚠️ **7 ve 8, 2026-08-20'de eklendi (M8-05 FAZ B1, [ADR 0017](0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md)).**
> Yukarıdaki *"Append kuralı"* gereği yeni bir ADR açılmadı. İkisi de **encode
> sürecine** aittir — yani ilk altısından farklı olarak bir *tap* riski değil,
> bir **üretim/kişiselleştirme** riskidir. 🔴 **Ve ikisi de bu belgenin genel
> kalıbını bir yerde bozuyor: doğrudan bir tespit sinyalleri YOK.** Bu, bağlam
> bölümündeki *"her risk … en azından gözlemlenebilir bir sinyale bağlanır"*
> ifadesinin **ilk istisnasıdır** ve gizlenmiyor — sinyal sütununda ne varsa
> **dolaylı** olduğu yazılıdır.

| # | Risk | Neden çözemiyoruz (hangi kırmızı çizgi) | Tespit sinyali | Uygulandığı görev |
|---|---|---|---|---|
| 1 | **Buddy punching** (A4/Q19) | Tek çözümü biyometri (§4.1) **veya** uygulama kurulumu — ikisi de ürünün varlık sebebine aykırı | Eş-zamanlı tap çiftleri raporu | [M6-11](../plan/m6-dashboard.md) |
| 2 | **Sahte GPS** (A3) | Web'de konum doğrulaması yok; attestation **uygulama** ister (app-less vaadi) | GPS-only tap oranı, çalışan kırılımında | [M6-11](../plan/m6-dashboard.md) |
| 3 | **URL biriktirme** (A1/Q21) | SUN payload'ında **zaman yok**; uçak modunda dokunup URL toplamak mümkün, sunucu o okumaları hiç görmez | `tap:ctrGap` → `base:ctr-gap-review`; boşluk metriği | baseline [M3-06](../plan/m3-policy-motoru.md) · metrik [M6-11](../plan/m6-dashboard.md) |
| 4 | **Mekânda bırakılmış proxy** (Y-E) ⚠️ *2026-08-19'da DARALTILDI — §4'ün ek notu* | IP taşınabilir; mekânın WiFi'ında proxy+VPN, uzaktaki tap'i mekânın IP'sinden gösterir. Kriptografik çözümü yok. ⚠️ Bunun altındaki **ayrı** bir yüzey — panelden `/0` vererek **yapılandırmayla üretilen** sahte IP kanıtı — kabul edilmiş DEĞİLDİR ve M8-04'te kapatıldı | `tap:gpsConflict` → `base:gps-conflict-review`; "IP eşleşti ama GPS uyuşmuyor" metriği | baseline [M3-06](../plan/m3-policy-motoru.md) · metrik [M6-11](../plan/m6-dashboard.md) |
| 5 | **Müdürün kimlik basması** (Y-D) | Davet kodunu üreten ve gören kişi = bordroyu şişirmede en güçlü teşviği olan kişi | Tek cihaz/oturumdan aktive N çalışan + hiç çapraz-lokasyon göstermeyen çalışan | rapor [M6-11](../plan/m6-dashboard.md) · davet kanalı [M5-02](../plan/m5-tap-akisi.md) (Q02) |
| 6 | **Fiziksel plaket devri** | Plaket duvardan sökülüp taşınabilir; pasif çipin "yerini" doğrulayan bir bağı yok | Lokasyon–IP/GPS uyumsuzluğu → `flag`; müdür `lost` işaretler | baseline [M3-06](../plan/m3-policy-motoru.md) · guardrail [M3-05](../plan/m3-policy-motoru.md) |
| 7 | **Encode oturumunun APDU dökümü** (ADR 0017 §2.2) | Boş çipte kimlik doğrulama anahtarı **fabrika varsayılanıdır, yani halka açıktır**; oturum anahtarları ondan türer. Dökümü gören taraf `ChangeKey` gövdesini çözüp o plaketin anahtarını öğrenir. **Çipin kişiselleştirme protokolünün yapısal özelliği** — altı kapatma yolu denendi, altısı da başarısız (ADR 0017 §2.2). 🔴 **KAPSAM 2026-08-21'DE BÜYÜDÜ:** ADR 0017 §6 md. 13 `FileAR.Write`/`ReadWrite`/`Change`'i **aynı `0x01` anahtarına** bağladı, yani sızan şey artık yalnız SUN imzalama yetkisi değil, **dosya YAZMA ve YENİDEN YAPILANDIRMA yetkisi**dir | 🔴 **Doğrudan sinyal YOK, ve sonuç iki dallı:** *(a)* **sahte SUN** — burada §5'in diğer üç kanıtı (oturum çerezi · IP · GPS) bağlamaya devam eder → kanıtsız tap `flag`'lenir · *(b)* 🔴 **NDEF'i repointleme (oltalama)** — md. 13'ten sonra mümkün, ve burada **üç kanıt HİÇ devreye girmez**: repointlenen plaketin tap'i **bize hiç ulaşmaz** (risk 8'in ölçümü) | Karşı önlem tespit değil **kapsama**: encode kontrollü ortamda, plaket duvara çıkmadan; şüphede ADR 0003 md. 5 → **`retire + replace`**. 🔴 **Maruziyet ilk encode turuyla SINIRLI DEĞİL** — her **kurtarma** turu da fabrika anahtarı altında koşar (ADR 0017 §5.3) |
| 8 | **Anahtar 0 fabrika varsayılanında kalan plaket** (ADR 0017 §5.0) — ⚠️ **2026-08-21'de DARALDI: oltalama yarısı kapandı, hizmet reddi yarısı duruyor** | İkinci bir plaket-başına anahtarı saklayacak şema yok (`tags` tek `aes_key_ref`, ADR 0003 md. 4); karar verilene kadar uygulama anahtarı 0 **halka açık** kalır. ⚠️ ADR 0017 §6 md. 13 (FAZ B2b) `FileAR.Write`/`ReadWrite`/`Change`'i **anahtar `0x01`**'e kilitledi → **elinde YALNIZ halka açık fabrika anahtarı 0 olan** tarafa karşı **NDEF yazılamaz ve SDM kapatılamaz**. 🔴 **Risk 7'nin kümesine karşı DEĞİL:** `0x01` her encode ve her kurtarma oturumunda o dökümü görene sızar ve md. 13'ten sonra **aynı zamanda yazma yetkisidir**. **Kalan:** aynı kişi anahtar 0'ı (ve 2–4'ü) üzerine yazabilir → **hizmet reddi** ve gelecekteki anahtar-0 kişiselleştirmemizin **önden alınması** | 🔴 **Tespit sinyali YOK, ve sebebi risk 6'dan farklı:** repointlenen plaketin tap'i **bize hiç ulaşmaz**, yani konum çelişkisi de dahil hiçbir kayıt doğmaz. Dolaylı işaret (plaketin sessizleşmesi) **yapısal olarak yetmiyor**: `ListTagLastSeen` satırı **yalnız en az bir tap üretmiş** plaket için doğar (ölçüldü 2026-08-20, yerel DB: 137 699 plaketin 79 995'i, yani **57 704'ü hiç satır üretmiyor**), yani **ilk tap'inden önce** repointlenen plaket stok plaketten ayırt edilemez | Şema kararı [M8-05](../plan/m8-deploy-pilot.md) FAZ B md. 9 · pilot kapısı Q23 md. 7 · *"anahtar 0 fabrikadayken plaket duvara çıkamaz"* güvenlik çizgisi (`deploy/README.md`) |

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

> 🔴 **EK NOT — 2026-08-19, M8-04 güvenlik denetimi (FAZ B2). Yukarıdaki kabul
> DARALTILDI, iptal edilmedi.** Bu bölüm silinmiyor (append kuralı); eklenen şey,
> risk 4'ün altında **ayrı bir yüzey** olduğunun ve o yüzeyin **kabul edilmediğinin**
> yazıya geçmesidir.
>
> **Ölçülen.** Kabul yukarıda *"20 €'luk bir telefon gerekir"* diye gerekçelendirilmiş.
> Denetim bunu çürüttü: **donanım gerekmiyordu.** Bir müdür panelden mekânın adres
> aralığını `0.0.0.0/0` yapabiliyordu ve sonraki **her** dokunuş — dünyanın herhangi
> bir yerinden — şu satırı yazıyordu: `verdict=ok trust=70 ip_match=t`,
> `note="network proof of place: the source IP matches the location"`. Eşleşmeyecek
> adres yok; yani cümle bir olguyu **beyan ediyor**, arkasında hiçbir kanıt yok. Ve
> `transactions` [CLAUDE.md](../../CLAUDE.md) §4.3 gereği **değişmez**, yani o satırlar
> sonradan düzeltilemez. Üstelik GPS izni verilmemişse `tap:gpsConflict`
> **tetiklenmez** → bu bölümün "tespit sinyali" olarak saydığı tek şey **sessiz** kalır.
>
> **Neden ayrı bir yüzey.** Bu ADR'nin kabul ettiği şey **taşınabilir bir IP
> kanıtıydı**: saldırganın mekâna fiziksel bir cihaz bırakması, yani bir maliyet ve
> bir fiziksel iz gerektiren bir saldırı. `/0` ise **yapılandırmayla üretilen sahte
> bir IP kanıtı** — sıfır maliyet, sıfır fiziksel iz, tek bir panel alanı. İkisi aynı
> kefeye konamaz.
>
> **Kapatıldı.** M8-04'te venue doğrulaması artık **her adresi eşleştiren** bir aralığı
> reddediyor: tek girdilik `/0` **ve** boşluk bırakmayan **birleşim**
> (`0.0.0.0/1,128.0.0.0/1`). Kural `netx.TooWideForProofOfPlace`'te (saf bir paket:
> `net/netip` + `math/big`, DB/HTTP/saat yok), hem alan sınırında
> (`internal/handler/locations.go`) hem etki alanında (`internal/domain/tenant/venue.go`)
> hem de **okuma tarafında** (`internal/domain/tap`'ın `ipMatches`'i) uygulanıyor;
> ölçüm ve sayılmış sınırlar o fonksiyonun başlığında.
>
> 🔴 **VE BU PARAGRAF BİR DÜZELTME TAŞIYOR — İLK HÂLİ ÜÇ GÜN DEĞİL, BİRKAÇ SAAT
> DAYANDI.** Şöyle diyordu: *"gelişim veritabanında `/0` taşıyan satır **0** ölçüldü
> (196 561 aralıklı mekân), bu yüzden **migration gerekmedi**"*. Adını verdiği
> veritabanında **2026-08-19 21:40 UTC** itibarıyla yeniden ölçüldü:
>
> | ölçüm (2026-08-19 21:40 UTC) | değer |
> |---|---|
> | aralık taşıyan mekân | 199 562 (309 518 mekânın içinde) |
> | maske uzunluğuna göre girdi | `/0`: 3 · `/1`: 2 · `/24`: 199 549 · `/29`: 12 · `/32`: 9 |
> | **evrensel aralık taşıyan mekân** | **4** (üçü tek `/0`, biri `/1 + /1` birleşimi) |
>
> 🔴 **VE BU TABLO DA BİR GÜN SONRA BAYATLADI — sayı 0 → 4 → 15 → 30 diye gitti.**
> Aynı veritabanında, **2026-08-20 son ölçüm**: aralık taşıyan mekân **208 476**
> (323 914 içinde); girdiler v4 `/0`: 23 · `/1`: 10 · `/24`: 208 437 · `/29`: 33 ·
> `/32`: 9, v6 `/0`: 2; **evrensel listeli mekân 30** (22'si tek `0.0.0.0/0`, 2'si
> `::/0`, 5'i `/1 + /1`, 1'i `/24`'ün yanında bir `/0`). Hepsi bu kartın **kendi
> mutasyon ve denetim koşularının kalıntısı** — ve ad sayısı da yanlış yazılmıştı:
> **iki değil ÜÇ ad** (`universal` 15, `St Julians` 11, `Everywhere` 4), ve
> *"on üç saniye"* diye yazılan ilk küme gerçekte **on üç MİLİSANİYE** içindeydi
> (2026-08-19 21:01:37.404–.417). **Silinmediler** — paylaşılan bir veritabanında
> satır sayısı bir belgeyi düzeltmek için azaltılmaz. **Bu satırlardaki hiçbir sayıyı
> bir kapı korumuyor**; tarihli birer gözlemdir, hedef değil — **ve adlar da öyle**,
> ki bu 4. turda öğrenildi: sayılar için kurulan "tarihli gözlem" çerçevesi
> **adları kapsamıyordu** ve tam orada yanlış çıktı.
>
> ⚠️ **"Erişilemezler" cümlesi ARTIK DOĞRU DEĞİL, ve kopyalanmak yerine yeniden
> ölçüldü.** Önceki hâli o lokasyonlarda 0 plaket, o tenant'larda 0 işlem diyordu.
> Bugün: o mekânlarda **11 aktif plaket**, orada kaydedilmiş **11 işlem**, ve
> bunların **4'ü `ip_match = TRUE`**. O dört satır tam olarak bu onarımın var olma
> sebebidir: *"network proof of place"* yazıyorlar, açık pencerede yazıldılar ve
> §4.3 gereği **hiçbir zaman düzeltilemezler**.
>
> 🔴 **Ama asıl düzeltme sayı değil, ÇIKARIM.** *"0 satır → migration gerekmedi"*
> yanlış bir gerekçeydi **sayı gerçekten 0 iken bile**, çünkü eksik olan yazma değil
> **okuma** tarafıydı: zaten yazılmış bir satır her yeni tap'te `ip_match=t, trust=70`
> üretmeye devam eder ve `transactions` **değişmezdir** (§4.3) — yani yalnız yazmayı
> kapatmak geçmişi **sonsuza kadar** açık bırakır. O yarı da kapatıldı:
> `tap.ipMatches` artık **yazma tarafının bugün reddedeceği** bir listeyi kanıt
> saymıyor. Bir migration yalnızca **ölü satırları temizlerdi**; güvenlik eklemezdi,
> bu yüzden yazılmadı.
>
> 🔴 **BİRLEŞİM YAZIMI DA KAPATILDI — VE ÖNCE "SAYILMIŞ LİMİT" DİYE SEVK EDİLMİŞTİ.**
> Bir tur boyunca burada *"birleşim yalnız yazma tarafında kapalı; kapsamı toplamak
> karar motorunun depolama katmanını içe aktarması demek"* yazıyordu. İtiraz doğruydu
> (§3: `internal/domain/tap` saf bir fonksiyondur, depolamayı bilmez) ama **ihtiyaç
> duyulan şey paket değil algoritmaydı**: `TooWideForProofOfPlace` saf önek aritmetiğidir
> ve kendi paketine (`internal/netx`) taşındı — iki taraf da oradan çağırıyor, tap
> hâlâ hiçbir depolama paketini içe aktarmıyor.
>
> **Limitin bedeli ölçüldü ve iddia ettiğinden büyüktü.** Sevk edilmiş `ipMatches`
> geri alınıp mutasyon koşuldu: yalnız `0.0.0.0/1 + 128.0.0.0/1` taşıyan bir mekânda
> **QR** kanalından, **GPS olmadan** bir tap → `verdict=ok, ip_match=TRUE, trust=70`.
> Yani yazılı limit, sahte bir IP kanıtı üretmekle kalmıyor, **`base:qr-requires-ip`
> baseline kuralını tümden devre dışı bırakıyordu** — oysa §5: *"QR: IP zorunlu, GPS
> tek başına yetmez → flag"*. Bugün aynı iki şekil: GPS'siz `flag`
> (`base:no-evidence-review`, §5 satır 7), GPS eşleşmesiyle `flag`
> (`base:qr-requires-ip`); **her ikisinde de kayıt yazılıyor** (§4.6). Kilit testler:
> `TestDecide_AStoredRangeTooWideForProofOfPlaceDoesNotMatch` ve
> `TestDecide_AStoredRangeTooWideForProofDoesNotBuyAPassAroundQRRequiresIP`
> (ikisi de 5. turda yeniden adlandırıldı; gerekçe aşağıda).
>
> ⚠️ **DAVRANIŞ DEĞİŞİKLİĞİ, ADIYLA:** kural artık **listenin**, tek girdinin değil.
> `203.0.113.0/24` **yanında** bir `0.0.0.0/0` taşıyan bir mekân eskiden `/24`
> üzerinden eşleşiyordu; o liste bugün **kaydedilemiyor**, dolayısıyla okuma tarafı da
> aynı cevabı veriyor. Yön **fail-closed**: tap GPS'e ya da §5 satır 7'ye düşer, kayıt
> her hâlükârda yazılır (§4.6). Bedeli ölçüldü ve **yeniden** ölçüldü (2026-08-20):
> evrensel aralık taşıyan **30** mekânın **1'i** ayrıca sıradan bir aralık taşıyor
> (`192.168.1.0/24` + `0.0.0.0/0`). İlk hâli *"4 mekânın hiçbiri"* diyordu; sayı da,
> "hiçbiri" de bayatladı — bu satır o yüzden tarihli.
>
> 🔴 **VE 4. TURDA YÜKLEMİN KENDİSİ TAŞINDI: "HER ADRES" DEĞİL, "BİR İSTEMCİNİN
> SUNABİLECEĞİ HER ADRES".** Denetim panele **sekiz satır** yapıştırdı —
> `11.0.0.0/8 · 8.0.0.0/7 · 12.0.0.0/6 · 0.0.0.0/5 · 16.0.0.0/4 · 32.0.0.0/3 ·
> 64.0.0.0/2 · 128.0.0.0/1` (sınır 32 girdi, yani **dörtte biri**) — yazma tarafı
> **kabul etti** (`n=8 universal=false`), ve `203.0.113.7`'den gelen bir tap
> **iki kanalda da** `verdict=ok · ip_match=true · trust=70` döndü; QR satırı 3. turun
> kapattığı çıktının **birebir aynısıydı**, yani `base:qr-requires-ip` yine devre
> dışıydı. Dışarıda bırakılan tek blok `10.0.0.0/8`: **RFC 1918**, hiçbir istemcinin
> public bir ingress'e sunamayacağı adresler. Yani liste **hiç kimseyi elemiyordu**,
> toplam ise 16 777 216 adres elendiğini söylüyordu. Doğrudan bir `/1` kabul etmek
> **meşrudur** (gerçek istemcilerin yarısını gerçekten eler); bu liste **sıfırını**
> eliyordu — derece değil **cins** farkı.
>
> **Yüklem artık istemci uzayında hesaplanıyor** (`netx.TooWideForProofOfPlace`):
> RFC 1918 · loopback · link-local · CGNAT · multicast · ayrılmış · `0.0.0.0/8` ve
> IPv6 karşılıkları toplamdan **düşülüyor**. **Yanlış pozitif ölçüldü:** 323 914
> mekânın sakladığı **19 farklı liste** yeni yüklemden geçirildi — **eskiden reddedilen
> 30, bugün reddedilen 30, yeni reddedilen 0**. Aynı oturumun sonunda yeniden koşuldu
> (326 397 mekân, yine **19** liste): reddedilen **36**, ama **reddedilen LİSTELER
> aynı dört şekil**. Sayı büyüdü, **küme büyümedi** — ki bu, "kalıntı üretiliyor"
> açıklamasının öngördüğü şeydir, "yanlış pozitif" açıklamasının değil.
>
> ⚠️ **VE BU ÇERÇEVENİN KENDİ BEDELİ, SAYILMIŞ HÂLDE:** uzay **public** istemci
> uzayıdır. Tüm istemcileri özel adreslerden gelen bir kurulum (bu veride
> **66 329** mekân `192.168.1.0/24`, **467** mekân bir `10.x` aralığı kaydetmiş) bu
> ölçünün kapsamında **değil**: `10/8 + 172.16/12 + 192.168/16`'nın tamamını kapsayan
> bir liste orada kimseyi elemez ve **kabul edilir**. Kapatılmadı, çünkü bir tenant'ın
> hangi kurulum şekli olduğunu bu paket bilemez. Aynanın öbür yüzü de yazıldı:
> public uzayı kapsayıp özel bir bloğu dışarıda bırakan bir liste, tamamen özel bir
> kurulumda herkesi elese de **reddedilir** — 323 914 mekânın **0**'ı böyle bir liste
> yazmış, ve o şekil zaten bu onarımın var olma sebebi.
>
> 🔴 **VE 5. TURDA YÜKLEM ÜÇÜNCÜ KEZ AŞILDI — BU KEZ AD LİSTESİ BIRAKILDI.** Yukarıdaki
> "istemci uzayı" çerçevesi bir **ad listesine** (13 blok) bağlıydı, ve bir uzayı
> **isim sayarak** tanımlayan her yüklem gibi, listede olmayan her blok bir kaçış
> üretiyordu. Ölçüldü, aynı ağaçta, aynı boyda iki yeni yazımla:
>
> | yazım | eski yüklem | kaç adres |
> |---|---|---|
> | `10.0.0.0/8` tümleyeni (8 satır) | reddediyor (10/8 listede) | 4 278 190 080 |
> | `25.0.0.0/8` tümleyeni (8 satır) | **kabul ediyor** | 4 278 190 080 |
> | `192.0.2.0/24` tümleyeni (24 satır) | **kabul ediyor** | 4 294 967 040 |
>
> Son satır hiç kimseyi elemiyor: `192.0.2.0/24` **RFC 5737 TEST-NET-1**'dir, hiç
> yönlendirilmez. Üçü de `verdict=ok · ip_match=true · trust=70` üretiyordu, **QR
> dahil** — yani `base:qr-requires-ip` yine devre dışıydı.
>
> **Yüklem artık bir BÜYÜKLÜK kuralı** (`netx.TooWideForProofOfPlace`; ad da değişti,
> çünkü "her istemci adresini kapsıyor" bu üç yazımın ikisi için **yanlıştı**): bir
> liste, aile başına **bir ISP tahsisinin iki katından** fazlasını kapsıyorsa yer
> kanıtı değildir — IPv4'te bir `/7` (2^25 adres, birim bir `/8`), IPv6'da bir `/31`
> (2^97, birim bir `/32` = bir RIR'ın LIR'a verdiği asgari tahsis). İkiye katlama
> süs değil: tamamen özel bir kurulum (`10/8` + `192.168/16`) çıplak bir `/8`'i
> **65 536 adresle** aşıyor ve bir yuvarlama yüzünden reddedilemez.
>
> **Yanlış pozitif ölçüldü, tüm mekânlara karşı** (2026-08-20): sevk edilen fonksiyon
> **her farklı listeye** koşuldu, mekân sayısıyla ağırlıklandırıldı — **332 855**
> mekân, **214 115**'i aralıklı, aralarında **19 farklı liste**. En geniş **gerçek**
> liste `{81.240.16.8/29, 192.168.1.0/24}` = **264 adres** (44 mekân); limit onun
> **127 100 katı**. Reddedilen **4 liste / 48 mekân**, ve 48'inin **48'i** bu kartın
> kendi kalıntısı (`{0.0.0.0/0}`×40, `{0.0.0.0/1,128.0.0.0/1}`×5, `{::/0}`×2,
> `{192.168.1.0/24,0.0.0.0/0}`×1; adları da söylüyor: *universal* 33, *St Julians* 11,
> *Everywhere* 4). **Gerçek mekân reddi: 0.** ⚠️ Sayılar her koşuda büyüyor (aynı tur
> içinde 331 499 → 332 855); **küme** büyümüyor — 4. turun reddettiği aynı dört şekil.
>
> ⚠️ **KALAN RİSK, SAYILMIŞ HÂLDE.** Limite tam oturan bir liste IPv4'ün **1/128**'ini
> kapsar — yani dünyanın herhangi bir yerinden gelen 128 istemciden biri yine
> eşleşir. Bu, paketin **söyleyebileceği** bir sınırdır, kapatabileceği bir delik
> değil: bir mekânın ağının ne kadar dar olduğu **binaya** dair bir olgudur. Kapanan
> şey, cevabın "esasen herkes" olduğu sınıfın tamamı (yukarıdaki üç yazım 5 alakasız
> public kaynağın 5'ini eşliyordu; limitteki bir liste beklenen değerde 640'ta 5).
>
> **Kalan risk DEĞİŞMEDİ.** Fiziksel proxy hâlâ kabul edilmiş durumda ve yukarıdaki
> tespit sinyali hâlâ onun sinyali. Bir `/8` — bir ISP'nin tüm tahsisi — hâlâ
> **kabul ediliyor**; değişen, artık bunun bir **cümle** değil bir **eşik** olması.
> ⚠️ Bir önceki tur bu satırda *"mekânın gerçek ağından çok daha geniş bir aralık da
> hâlâ kabul ediliyor"* ve *"T40'ın bu iki alt sorusu karara bağlanmadı"* diyordu;
> 5. turda **bağlandı** — eşik yukarıdadır.

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

### 7. Encode oturumunun APDU dökümü (ADR 0017 §2.2)

**Neden çözemiyoruz.** Boş bir NTAG 424 DNA çipinde uygulama anahtarı 0 **fabrika
varsayılanıdır** ve o değer halka açıktır. `AuthenticateEV2First`'ün oturum
anahtarları o anahtardan ve iki rastgele sayıdan türer; ikisi de o anahtar altında
şifreli olarak telde gider. Dolayısıyla kişiselleştirme oturumunun APDU dökümünü
gören biri oturum anahtarlarını yeniden türetebilir ve `ChangeKey` gövdesini
çözebilir — yani o oturumda yazılan **plaket anahtarını öğrenir**.

🔴 **Bu bir uygulama kusuru değil, protokolün şeklidir**, ve kapatılabileceğini
düşünen bir sonraki okur zaman kaybetmesin diye **altı deneme ve altı başarısızlık**
ADR 0017 §2.2'de sayılıdır (önce anahtar 0'ı değiştirmek · anahtarı türetip
göndermemek · röleyi iki cihaza bölmek · anahtarı çipte üretmek · sonradan
güvenilir okuyucuyla yeniden anahtarlamak · ikinci kanaldan doğrulamak). Genel
ifade: **halka açık bir anahtarla başlayan bir kanal üzerinden gizlilik bootstrap
edilemez.** Aynı sınır USB okuyucu yolunda da vardır; röle onu yaratmaz, yalnız
dökümün geçtiği yüzeyi genişletir.

**Tespit sinyali — dürüst cevap: doğrudan yok.** Sızan anahtar, sahibine o plaket
için geçerli SUN üretme yeteneği verir; bu, kayıt akışında **meşru bir tap gibi**
görünür. Kalan bağ dolaylıdır: §5'in diğer üç kanıtı (oturum çerezi = kim ·
IP = nerede · GPS = yedek nerede) bağlamaya devam eder, yani kanıtsız bir tap
yine `flag`'lenir. Bu bir tespit değil, bir **sınırlama**dır.

🔴 **VE 2026-08-21'DE BU PARAGRAF YETERSİZ KALDI — GÜVENLİK DENETİMİ ÖLÇTÜ.**
Yukarısı sızan anahtarın sonucunu **yalnız "sahte SUN üretmek"** diye yazıyor ve
azaltmayı *"§5'in diğer üç kanıtı bağlamaya devam eder"* diye veriyor. ADR 0017
§6 md. 13'ün kararından sonra **ikisi de eksik**:

- **Sonuç büyüdü.** `K_0x01` artık aynı zamanda `FileAR.Write`, `FileAR.ReadWrite`
  ve `FileAR.Change`'tir. Dökümü gören taraf o anahtarla
  `AuthenticateEV2First(0x01)` yapabilir (§5.3'ün 2 numaralı sondası zaten bu
  şekildedir), sonra `WriteData` ile **NDEF URL'sinin host'unu değiştirebilir** ve
  `ChangeFileSettings` ile **SDM'i kapatabilir**. Yani sonuç *"sahte SUN"* değil,
  **"sahte SUN VEYA oltalama"**dır.
- **Azaltma cümlesi o dalda GEÇERSİZ.** Üç kanıt yalnız **bize ulaşan** bir tap'i
  bağlar. Repointlenen bir plakette çalışanın telefonu **saldırganın sitesini**
  açar; bize hiçbir kayıt gelmez, dolayısıyla çelişecek bir IP, GPS ya da çerez
  de yoktur. Bu, risk 8'in kendi ölçümüyle aynı şeydir ve orada *"tespit sinyali
  yok"* diye yazılıdır.

⚠️ **Bu, md. 13'ün kararını yanlışlamıyor.** Alternatifleri daha kötü: anahtar
`0x00` bugün **halka açık**, teslim değeri `Eh` **herkese açık**, `Fh` ise Q08 ve
§5.3 gerekçesiyle reddedildi. Değişen şey **kimin yapabildiğidir**: *"veri
sayfasını okumuş herkes"*ten *"belirli bir encode oturumunun dökümünü görmüş
taraf"*a. Bu gerçek bir daralmadır; **kapanma değildir**, ve risk 8'in tablosu
artık bunu niteleyicisiyle yazıyor.

> 📍 **YETKİLİ METİN: [ADR 0017](0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md)
> §2.2 (tehdit modeli, altı başarısız kapatma denemesi) ve §5.3 (kurtarmanın
> maruziyeti).** Aşağısı sicil için yapılmış bir **özettir**. İkisi çelişirse
> **ADR 0017 geçerlidir** ve bu satır o gün düzeltilmelidir — bu dosyanın kendi
> ilkesi (`deploy/README.md`: *"iki yerde duran bir ölçü er geç iki farklı ölçü
> olur"*), ve bu risk bugün **dört ayrı dosyada** anlatılıyor.

**Karşı önlem kapsamadır, tespit değil.** Encode **kontrollü ortamda**, **bizim
cihazımızla**, plaket **duvara çıkmadan önce** yapılır. Etkilenen küme o
oturumlarda encode edilen plaketlerdir (ADR 0003 md. 3 gereği **park geneli bir
sır yoktur** — yarıçap plaket başınadır). Şüphe hâlinde yol ADR 0003 md. 5'in
normatif yoludur: **`retire + replace`**.

🔴 **VE YÜKÜMLÜLÜK İLK ENCODE TURUYLA BİTMEZ — KURTARMA DA KAPSAM İÇİNDEDİR.**
ADR 0017 §5.3: yarım kalmış bir çipin kurtarılması *fabrika → bizimki* yönünde
bir `ChangeKey` gerektirir ve o oturum, ilk turdan **günler sonra** ve muhtemelen
**başka bir telefonla**, yine **halka açık** anahtar 0 altında koşar. Yani
maruziyet penceresi *"encode günü"* değil, **o plakete yapılan her fabrika-
anahtarlı oturumdur**; *"kontrollü ortam"* şartı kurtarma için de geçerlidir.
(Teşhis sondaları maruz **değildir**: biri bizim anahtarımızla koşar, öteki
fabrika anahtarıyla koşar ama hiçbir gövde göndermez.) ⚠️ *"Kontrollü ortam"* bugün bir **temenni**dir, bir mekanizma değil:
ağaçta encode'a özgü cihaz kimliği, izin listesi ya da ağ kısıtı **yok**
(ADR 0017 §6 md. 10).

### 8. Anahtar 0 fabrika varsayılanında kalan plaket (ADR 0017 §5.0)

> ⚠️ **BU RİSK 2026-08-21'DE DARALDI VE AŞAĞISININ BİR KISMI O TARİHTEN ÖNCEYE
> AİTTİR.** M8-05 FAZ B2b, ADR 0017 §6 md. 13'ü kapatarak `FileAR.Write`,
> `FileAR.ReadWrite` **ve** `FileAR.Change`'i plaket-başına anahtar `0x01`'e
> kilitledi. **Sicile satır EKLENMEDİ** — sekiz risk, sekiz satır; değişen bu
> riskin **kapsamıdır**. Ne kaldığı ve ne gittiği aşağıda *"Ne daraldı"* alt
> başlığında **tek tek sayılıdır**; oradan önceki paragraflar riskin **tam**
> hâlini anlatır ve tarihsel kayıt olarak **silinmedi** (bu belgenin append
> kuralı).
>
> 📍 **YETKİLİ METİN: [ADR 0017](0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md)
> §5.0 (anahtar numaraları kararı ve oltalama ölçümü) ve **§6 md. 13** (erişim
> hakları kararı, 2026-08-21).** Güvenlik çizgisinin
> operasyonel hâli `deploy/README.md` → *"FAZ B'ye devredilenler"* md. 9'da,
> kapısı [M8-06](../plan/m8-deploy-pilot.md)'nın Q23 listesi md. 7'de. Aşağısı
> sicil için yapılmış bir **özettir**; çelişkide **ADR 0017 geçerlidir**.

**Neden kabul ediyoruz.** ADR 0017 §5.0 uygulama anahtarı 0'ın da
kişiselleştirilmesini **karara bağladı**; sevk edilmesi bir **şema kararına**
bağlı, çünkü `tags` tek bir `aes_key_ref` taşır ve ADR 0003 md. 4 onu tam 44
bayta — yani **tek** bir AES-128 anahtarına — sabitler. Üç saklama şıkkı
(`deploy/README.md` → FAZ B md. 9) sayıldı, hiçbiri seçilmedi. Karar kapanana
kadar encode edilen plaketlerde anahtar 0 **halka açık** kalır.

**Somut sonucu oltalama.** Plakete fiziksel erişimi olan biri master olarak kimlik
doğrulayıp NDEF URL'sinin host'unu değiştirebilir; çalışan plakete dokunur ve
telefonu **saldırganın sitesini** açar. Veri sayfası bunu kendi ayak notunda
emrediyor (tablo 8: *"Write and ReadWrite access rights for the NDEF File
should be changed after personalization"*), ve gereken araç özel donanım değil —
`deploy/README.md`'nin *"Dört yol"* ölçümü bir mağaza uygulamasının (NFC.cool
Tools) `ChangeKey` **ve** `ChangeFileSettings`'i telefonda yaptığını kaydetti.

🔴 **Tespit sinyali YOK — ve bu, risk 6'dan ayrıldığı yerdir.** Risk 6'da
(fiziksel plaket devri) taşınan plaket **hâlâ bize tap üretir**, yalnız konum
kanıtı çelişir → `flag` → müdür `lost` işaretler. Burada öyle değil: NDEF'i
saldırganın host'una çevrilmiş bir plaketin tap'i **hiç bize ulaşmaz**, yani
çelişecek bir kayıt bile doğmaz. Aynı fiziksel erişim, **sinyalsiz** sonuç.
Dolaylı tek işaret plaketin **sessizleşmesi** olurdu — ve **o da yapısal olarak
yetmiyor**, ölçüldü (2026-08-20, güvenlik denetimi):

- `ListTagLastSeen` `transactions` üzerinde `tag_uid IS NOT NULL` ile **tek bir
  tarama** yapar ve `GROUP BY` kullanır; sorgunun kendi yorumu bunu açıkça
  yazıyor: *"a group EXISTS only when it has [rows]"*. Yani **satır yalnız en az
  bir tap üretmiş plaket için doğar.**
- 🔴 **Sonuç: ilk tap'inden ÖNCE repointlenen bir plaket için sinyal YOKTUR** —
  satır hiç doğmaz ve plaket, henüz taranmamış bir stok plaketten **ayırt
  edilemez**. Risk 8'in en olası senaryosu tam budur (yeni monte edilmiş plaket).
- Canlı ölçüm bunu doğruluyor — **tarihiyle, popülasyonuyla ve KİMİN ölçtüğüyle
  birlikte**, çünkü popülasyonsuz bir oran bu repoda sayılmaz
  (`db/queries/tags.sql`'in kendi kuralı):
  - **Ağaç geneli — 2026-08-20, yerel geliştirme veritabanı, `tappa_owner` ile
    salt-okuma sondası (`BEGIN … ROLLBACK`):** **137 699** plaketin **79 995**'i
    en az bir tap üretmiş → **%58**. Yani **57 704 plaket için `ListTagLastSeen`
    hiç satır üretmiyor.**
  - **Tek tenant kesiti — 2026-08-20, tenant
    `10000000-0000-4000-8000-000000000001`, bağımsız üçüncü göz tarafından
    `BEGIN … ROLLBACK` salt-okuma sondasıyla üretildi** (⚠️ **tam id yazılıyor**,
    kısaltma değil: yerel veritabanında id'si `1000` ile başlayan **beş** tenant
    var, yani *"tenant 1000…"* bir popülasyon adlandırmaz — ve bu paragrafın
    kendi kuralı popülasyonun adıyla yazılmasıdır)**:** o tenant'ın
    **25** plaketinin **4**'ünde `ListTagLastSeen` satırı var → **%16**.
    ⚠️ Bu satırı yazan tur rakamı **kendi sondasıyla üretemedi** (tenant
    filtresiz denemesi ağaç ölçeğinde zaman aşımına uğradı); **teyit ayrı bir
    ajandan geldi** ve rakam o yüzden burada duruyor — kim ölçtü, kaydın parçası.
  - Sorgunun kendi yorumu üçüncü bir veri kümesinde *"only 3 of the 11 plaques
    have ever been tapped"* diyor — aynı şekil, farklı ölçek.

  ⚠️ Bu oranlar **seed verisinin** özellikleridir, üretimin değil. Taşıyan şey
  oran değil **şekildir**: tap üretmemiş plaket satır üretmez, ve o şekil
  `GROUP BY`'dan gelir — veriden değil.
- Ayrıca eşik/uyarı **kodu yok** (sıfır isabet); değer yalnız panelde bir sütun,
  **tenant kapsamlı** — yani izleyecek olan **müdür**, biz değiliz.

⚠️ Bu yüzden bu maddenin başlığındaki *"Tespit sinyali YOK"* **kalibredir**;
*"veri mevcut ama uyarı yazılmadı"* şeklindeki daha yumuşak bir ifade **bir tık
geniş** olurdu ve geri çekildi.

**Ne daraldı (2026-08-21, M8-05 FAZ B2b) — tek tek.** ADR 0017 §6 md. 13
`FileAR.Write`, `FileAR.ReadWrite` **ve** `FileAR.Change`'in üçünü de
plaket-başına anahtar `0x01`'e bağladı.

> 🔴 **AŞAĞIDAKİ TABLONUN İKİ NİTELEYİCİSİ VAR VE İLK YAZIMINDA İKİSİ DE YOKTU
> (güvenlik denetimi, 2026-08-21).** Niteleyicisiz hâli iki ayrı şekilde fazla
> iddia ediyordu:
>
> 1. **KİM.** Tablo, elinde **YALNIZ halka açık fabrika anahtarı 0** olan tarafı
>    anlatır. **Risk 7'nin kümesini anlatmaz:** `K_0x01` her encode ve her kurtarma
>    oturumunun dökümüne sızar (risk 7) ve md. 13'ten sonra o anahtar **yazma ve
>    yeniden yapılandırma yetkisidir** — yani o taraf **oltalama yapabilir**.
> 2. **NE ZAMAN.** Daralma yalnız **adım 7 TAMAMLANDIKTAN sonra** geçerlidir.
>    Adım 5 ile 7 arasında kesilmiş bir çipte haklar hâlâ **teslim değerindedir**:
>    `Write = ReadWrite = Eh` (**serbest, hiçbir anahtar gerekmez**) ve
>    `Change = 0h`. O çipte oltalama ve SDM yapılandırması **tamamen açıktır**.
>    Süreç bunu kapsıyor — satır `unassigned`, `location_id NULL`, yani plaket
>    duvara çıkmamıştır (ADR 0017 §5.2) — ama **kapsayan şey süreçtir, çipin
>    ayarları değil**.

Bu iki niteleyici altında, plakete fiziksel erişimi olan ve elinde yalnız fabrika
anahtarı 0 bulunan biri, **adım 7 tamamlanmış** bir plakette:

| | Yapabilir mi | Neden |
|---|---|---|
| NDEF URL'sini değiştirmek (**oltalama**) | ❌ **Hayır** | `WriteData` `Write` **veya** `ReadWrite` ister, ikisi de `0x01` |
| SDM'i kapatmak / yeniden yönlendirmek | ❌ **Hayır** | `ChangeFileSettings` `Change` ister, o da `0x01` |
| Anahtar `0x01`'i öğrenmek ya da değiştirmek | ❌ **Hayır** | Veri sayfası tablo 63: gövde `New XOR Old` **ve** `New` üzerinde `CRC32` ister; eski anahtarı bilmeyen ikisini de üretemez, çip reddeder |
| Dosyayı okumak | ✅ Evet | `Read = Eh` **kasıtlı**; URL zaten halka açıktır (plakette basılı) |
| Anahtar 0'ı ve 2–4'ü üzerine yazmak | ✅ Evet | Eski değerleri halka açık sıfırdır, yani gövde hesaplanabilir |

🔴 **Kalan zararın adı: HİZMET REDDİ ve ÖNDEN ALMA**, sahtecilik değil. Anahtar
0'ı kendi değeriyle değiştiren biri, **bizim** gelecekteki anahtar-0
kişiselleştirmemizi (ADR 0017 §5.0 karar 2) ve kurtarmanın fabrika-anahtarlı
yarısını (§5.3 sonda 3) kalıcı olarak engeller. Çözüm yolu değişmiyor:
`retire + replace`. ⚠️ Ve **tespit sinyali bu yarı için de yoktur**: anahtar 0
değişse bile taplar **normal doğrulanır** (SUN anahtar `0x01` ile imzalanır), yani
dışarıdan hiçbir şey olmamış görünür.

⚠️ **BU DARALMA RİSKİ KAPATMIYOR ve şema kararının yerine geçmiyor** (ADR 0017 §6
md. 5).

🔴 **VE BİR YÜZEY *"ÖLÇÜLMEDİ"* DİYE YAZILMIŞTI — ÖLÇÜLDÜ (2026-08-21, bağımsız
üçüncü göz, veri sayfası elde), VE İÇİNDE GERİ ALINAMAZ BİR MADDE VAR.**
`SetConfiguration` veri sayfası **§10.5.1**'e göre *"requires an authentication
with the **AppMasterKey**"* — yani **halka açık fabrika anahtarı 0 ile
erişilebilir**. **Tablo 50**'nin seçenekleri:

⚠️ **VE BU LİSTE TAMDIR — ilk yazımı ÜÇ seçenek gösteriyordu ve kısmi olduğunu
söylemiyordu.** Tablo 50'nin **beş** seçeneği var; hepsi aşağıda:

| Seçenek | Ne yapar | Bizim için |
|---|---|---|
| `00h` PICC Configuration | `PICCConfig` bit 1 → **Random ID** | ⚠️ **Tap'i KIRMIYOR** (s.12: *"it is still possible to enable plain PICCData mirroring of the UID even if Random ID is enabled"*) ama 🔴 **`GetVersion`'ın UID alanını SIFIRLAR** — Tablo 58: *"All zero — if configured for RandomID"*. Encode akışı o değeri `tags.uid`'e (**PRIMARY KEY**) yazsaydı ilk plaket işgal eder, sonrakiler çakışırdı; `internal/sun/apdu.go` artık **reddediyor** |
| `04h` Secure Messaging Configuration | `WriteData`'da zincirli yazmayı kapatabilir | dar: bizim yazmamız **tek çerçeve** |
| 🔴 `05h` Capability data → `PDCap2.1` **bit 1** | *"1b means enable LRP mode. **This change is permanent**, LRP mode **cannot be disabled** afterwards."* | 🔴 **KALICI**: LRP'de SDM MAC ayrı bir yolla hesaplanır (§9.3.8.2) → **doğrulayıcımızın asla okuyamayacağı bir plaket**, geri dönüşü yok. ⚠️ Seçeneğin adı *"LRP modu"* değil **"Capability data"**dır; LRP onun içindeki **bir bit**tir |
| `0Ah` Failed authentication counter | deneme limiti/azaltma | zarar dar |
| `0Bh` HW configuration | geri modülasyon gücü | okuma mesafesini bozabilir; kalıcı değil |

🔴 **VE AYNI TARAMA, md. 13'ÜN HİÇ KAPSAMADIĞI BİR YOL BULDU — sayılıyor.**
Md. 13 yalnız **dosya `02h`**'yi kilitler. Veri sayfası **tablo 8**: dosya `01h`
(**Capability Container** — telefona NDEF mesajının **nerede** olduğunu söyleyen
dosya) teslimde `Write = ReadWrite = 0h`, dosya `03h` (proprietary) ise `3h`.
Anahtar `0x00` ve `0x02`–`0x04` fabrika sıfırında kaldığı sürece (ADR 0017 §6
md. 5 ikisini de açık bırakıyor) **ikisi de herkese yazılabilir**.

🔴 **VE BUNUN YARISI TÜRETME DEĞİL, YAYIMLANMIŞ BİR ÖRNEK — ilk yazımı bunu
bilmiyordu ve fazla temkinli etiketlenmişti (2026-08-21, üçüncü denetim).**
AN12196 rev. 2.0 **§5.14** doğrudan söylüyor: *"By default, CC file has
FileAR.ReadWrite set to 00. **Therefore Authentication with Key0 needs to be
done**."* — yani **halka açık anahtar 0 ile CC'ye yazmak belgenin kendi
kişiselleştirme dizisinde bir adımdır**. **§5.15 tablo 24** tam C-APDU'yu basıyor
(`Cmd.WriteData`, `FileNo = 01`, `Offset = 0E0000`, 18 bayt) ve yazdığı içerik
`05 06 E105 0080 82 83` — **`E105h`'i NDEF olmayan dosya olarak adlandıran
Proprietary-File_Ctrl_TLV'nin ta kendisi**. Türetilmiş kalan **tek** parça,
telefonun CC'deki dosya kimliğini izlemesidir (NFC Forum T4T davranışı).

**Saldırının tam adımları — ve biri ilk yazımda ATLANMIŞTI:**
1. Anahtar 0 (halka açık) ile kimlik doğrula → `E105h`'e istediği NDEF URL'sini
   yaz (`03h`'nin `Write`'ı `3h`'dir ve anahtar 3 de fabrika sıfırıdır).
2. 🔴 **ATLANAN ADIM:** `03h`'nin `Read`'i teslimde **`2h`**'dir (tablo 8), yani
   kimlik doğrulamamış bir telefon `E105h`'i **okuyamaz**. Saldırganın **ayrıca**
   `ChangeFileSettings` ile `03h`'nin `Read`'ini `Eh`'ye taşıması gerekir —
   **mümkün**, çünkü `03h`'nin `Change`'i teslimde `0h`'dır.
3. CC'yi (`01h`) `E105h`'i NDEF dosyası gösterecek şekilde yeniden yaz.
   Sonuç: telefon **saldırganın URL'sini** okur ve dosya `02h`'ye **hiç
   dokunulmaz**. ⚠️ Eksik adım sonucu değiştirmiyor ama sapma **yanlış yöndeydi**:
   cümle saldırıyı olduğundan **kolay** gösteriyordu.

🔴 **VE BU AİLENİN GERİ ALINAMAZ ÜYESİ — *"kalıcı"* listesine ait ve orada yoktu.**
`ChangeFileSettings` ile dosya `01h` ya da `03h`'nin **herhangi bir hakkını `Fh`**
yapmak (tablo 6: *"no access"*) **geri alınamaz**, çünkü o hakkı geri açacak komut
yine `ChangeFileSettings`'tir (tablo 9) ve onu yöneten `Change` hakkı da `Fh`
olmuştur — ve **veri sayfasında format/fabrika ayarlarına dönüş komutu yoktur**
(tablo 22 taraması: `FormatPICC`, `CreateApplication`, *factory reset*, *restore
defaults* → **sıfır eşleşme**). İki somut sonucu: *(a)* CC'yi repointledikten
**sonra dondurmak** → **düzeltilemez bir oltalama plaketi**; *(b)* boş bir çipte
`02h`'nin `Change`'ini `Fh` yapmak → **bizim adım 7'miz kalıcı olarak başarısız
olur** (ADR 0017 §5.3'ün 1 numaralı sondası bunu **görür** — `Change`'i okuyor).
Sınıf yine aynı: **hizmet reddi**, çare `retire + replace`.

**Kapatan şey md. 13 değil, anahtar `0x00`/`0x02`–`0x04`'ün kişiselleştirilmesi
ya da dosya `01h`/`03h`'in kilitlenmesidir**; ikisi de ADR 0017 §6 md. 5'e bağlı.

**Sonucu değiştirmiyor, sınıfını değiştirmiyor:** üçü de **sahtecilik değil**,
**hizmet reddi**dir ve çaresi aynıdır — `retire + replace`. Değişen tek şey
dürüstlüktür: bu satır artık *"ölçülmedi"* demiyor, **ne olduğunu** söylüyor, ve
LRP'nin **geri alınamaz** olduğunu kayda geçiriyor.

**Kapatma yolu ve kapı.** Şema kararı ADR 0017 §6 md. 5'te; yükümlülük
`deploy/README.md` → *"FAZ B'ye devredilenler"* md. 9'da; ve pilot kapısı
[M8-06](../plan/m8-deploy-pilot.md)'nın Q23 listesinde **7. madde** olarak
duruyor. Güvenlik çizgisi: encode aracının **inşası ve testi** bunu beklemez,
ama **bir plaket anahtar 0 fabrika varsayılanındayken duvara çıkamaz.**
⚠️ Bu çizginin bugün **mekanik karşılığı yoktur** — `AssignTagToLocation`'ın
WHERE'i yalnız `tenant_id` + `uid` + `status = 'unassigned'` bakar, anahtar 0
durumunu okuyan hiçbir sütun ya da kontrol ağaçta yok.

---

## Kabul edilen ORTA/DÜŞÜK denetim bulguları (M8-04 FAZ B3, 2026-08-20)

> **Bunlar yukarıdaki SEKİZ RİSKTEN AYRI bir sınıftır ve tabloya eklenmediler.**
> Risk 1–8 ürünün **kalıntı tehditleri**dir — bir saldırganın yapabildiği bir şey.
> ⚠️ *"ALTI"* 2026-08-20'de **sekiz** oldu (risk 7 ve 8, ADR 0017); bu paragrafın
> ayrımı değişmedi, yalnız sayısı.
> Aşağıdaki tablo M8-04 güvenlik denetiminin **kapatılmayıp KABUL EDİLEN** ORTA/DÜŞÜK
> bulgularını tutar: çoğu bir **kapının darlığı**, bir **geliştirme ortamı** koşulu ya
> da bir **dağıtım ön koşulu**. Karışmasınlar diye ayrı duruyorlar. ⚠️ Bu blok
> *"'Aşağıdaki **altı risk** kabul edilir' cümlesi değişmedi"* diyordu; **artık
> değişti** — Append kuralı gereği risk 7 ve 8 o tabloya eklendi ve cümle
> **sekiz**'e çıktı. Değişmeyen şey **bu tablonun ondan ayrı olmasıdır**.
>
> 🔴 **CÜMLE DARALTILDI, ÇÜNKÜ GENİŞ HÂLİ YANLIŞTI — ÖLÇÜLDÜ (2026-08-20, FAZ B3
> düzeltme turu).** İlk yazımı *"kapatılmayan ORTA/DÜŞÜK bulgularıdır"* diyordu, yani
> tablonun **kapatılmayanların tamamı** olduğunu iddia ediyordu; en az üç şey
> tablonun dışındaydı ve üçü de aynı sınıftan **değil**:
> **(1) T3** — kabul edilmiş, gerekçesi ve üç ölçümü **ağaçta**
> (`internal/handler/cookies.go`, `internal/session/cookie.go`), ama satırı yoktu →
> **eklendi**, çünkü kabul odur.
> **(2) T40'ın (ii) ve (iii) soruları** — *ölçüldü, karara bağlanmadı*
> ([m8-deploy-pilot.md](../plan/m8-deploy-pilot.md) M8-04 FAZ B2 bloğu). Bunlar
> **kabul değil**, açık karar; tabloya girerlerse "kabul edildi" diye okunurlar.
> **(3) FAZ A'nın F7'si** — metni repoda **yok**; olmayan bir bulgu kabul edilemez.
> **Kural şudur:** bu tablo *kabul edilenleri* tutar; **açık kalmış ve karara
> bağlanmamış** sorular görev kartında durur ve oraya taşınmazlar.
>
> ⚠️ **BU LİSTENİN DÖRDÜNCÜ BİR MADDESİ VARDI VE GERİ ÇEKİLDİ (düzeltme turu 2).**
> *"Tap sonuç ekranı katman etiketi taşımıyor"* bir **bulgu** olarak yazılmıştı;
> dayanağı, tenant'ın politika cümlesinin metnini kendisinin yazdığı iddiasıydı ve o
> iddia ölçülerek **çürütüldü** (`internal/domain/tenant`'ın `copyOfShipped`'i ifadeyi
> **bütün** kopyalar, `AuthorCommand`'da alan yok, panel formunda alan yok). Cümle
> her katmanda **bizimdir**; ekranda eksik olan tek şey *"kararı hangi belge verdi"*
> bilgisidir ve onu okuması gereken kişi çalışan değil **müdürdür** — fişte zaten
> basılıyor. Kabul edilecek bir risk kalmadığı için tabloya da girmiyor.
>
> **Neden burada ve neden `docs/backlog.md`'de durmaları yetmedi.** M8-04 kartının
> kabul kriteri şudur: *"ORTA/DÜŞÜK olanlar ya kapandı ya gerekçesiyle kabul edildi
> **ve yazıldı**."* Backlog **kullanıcı eylemi bekleyen** işleri tutar ve kendi
> başlığı bunu söyler; bir **kabul kararı** oraya yazıldığında karar değil bir
> hatırlatma olur. Kabul **ağaçta** durmalı.
>
> 🔴 **ÇAPALARIN ÇOĞU BİR TEST ADIDIR — AMA HEPSİ DEĞİL, VE FARK SAYILMIŞTIR.**
> `TestEveryNamedTestExists` (cmd/tappa) bu dosyayı da tarar — deposundaki **her
> metin dosyasını** tarar — yani aşağıda adı geçen bir test silinir ya da yeniden
> adlandırılırsa **bu belge kırmızıya döner**. Kabul metni ile onu doğru kılan
> mekanizma böylece ayrılamaz. Çapaların yönü de yazılıdır: çoğu, kabulün
> **önkoşulunu** tutar, yani önkoşul kalkarsa kabul de kalkar. Bir test adına
> bağlanamayan satırlar **`MEKANİK ÇAPASI YOK, SAYILDI`** damgasını taşır; gerekçe
> tablonun altında.
>
> **Bağlı sayılar.** `TestADR0005_TheAnchorCountsMatchTheProse` tabloyu **bu
> dosyadan** okur ve şunları doğrular: **satır = 13** · **çapasız satır = 3**,
> çapasızlar tam olarak **T28 · T38 · T39**, ve damgasız her satırın çapa hücresi
> **gerçekten bir `Test…` adı anıyor**.
>
> 🔴 **BU PARAGRAFIN İLK HÂLİ *"HER SATIR BİR MEKANİK ÇAPA TAŞIR"* DİYORDU, VE AYNI
> BELGE OTUZ SATIR AŞAĞIDA ONU ÇÜRÜTÜYORDU** (*"üç satırın mekanik çapası yok"*).
> Ağ tutuyordu; **ağ hakkındaki cümle yanlıştı** — bu görevde yedi kez bloklayan
> bulgu üreten sınıfın bu belgedeki üyesi. Sayılar artık yazılı değil **bağlı**
> (M8-04 FAZ B3 kapanışı, 2026-08-20).

| Bulgu | Ne kabul edildi | Neden kapatılmadı | Mekanik çapa (kabulü yanlışlayan) |
|---|---|---|---|
| **T3** çerez gölgeleme: alt alan adı sahibi aynı adla ikinci bir çerez yazabilir, `r.Cookie` **ilkini** döndürür ve hangisi olduğu ayırt edilemez | `__Host-` öneki **alınmadı**. Bugün duran şey: `SameSite=Lax` + `HttpOnly` + üretimde koşulsuz `Secure`, ve **dikilen bir SESSION değeri oturum değildir** — veritabanında çözülmek zorunda. Kapanmayan kısım oturum **FIXATION**'ıdır: saldırganın **kendi** oturumunu kurbanın tarayıcısına diktirmesi | Önek `Path=/` **şart koşar**; panel çerezi bilerek `/admin`'e daraltılmıştır ve bu daralık *"panel çerezi tap yüzeyine erişemez"* güvencesinin yapısal yarısıdır — önek ya onu bozar ya da çerezlerin bir kısmına uygulanır. Ayrıca önek `Secure` istediği için `TAPPA_ENV`'e koşullu olurdu; yanlış yapılırsa **yalnız üretimde** patlar. Gerçek çare ürünün **kardeşsiz bir konaktan** sunulmasıdır — bir kod değişikliği değil, **operatör kararı**. Üç ölçüm `internal/handler/cookies.go`'da | `TestPanelCookiePath_IsNarrowerThanTheTapSurface` (kabulün 2. dayanağı: önek tekbiçimli olamaz, çünkü panel yolu dar) + `TestCookies_ZeroValueIsSecure` (`internal/session`; 1. dayanağı: `Secure` yapısal — sıfır değer bile güvenli, yani üretimde Secure'suz çerez üretilemez) |
| **T27** faturalama: `plan` değişimi kapatılmamış ayları ücretliye çevirebilir | Bugün erişim dar: `plan`'ı **yalnız operatör** yazabilir | Çare bir **plan geçmişi tablosu** ya da bir **sıralama kısıtı** — ikisi de migration + faturalama kararı, pilot faturalaması başlamadan kullanıcının vereceği bir karar | `TestAccountDB_TheAppRoleHoldsNoUpdateOnTheVATColumnsOrTheTerms` — `tappa_app`'e `tenants.plan` üzerinde UPDATE verilirse kırmızı, ve **kabulün tek dayanağı** o yokluktur |
| **T28** uygulama kendi `Strict-Transport-Security` başlığını set etmiyor | Başlık **ingress'ten** geliyor (ölçüldü: `max-age=31536000; includeSubDomains`) | HSTS ingress-nginx'te **per-controller** bir ConfigMap seçeneği; paylaşılan ConfigMap'i değiştirmek ~20 başka uygulamayı etkiler (gerekçe `deploy/k8s/40-ingress.yaml` başında). `preload` bilinçli olarak **set edilmedi** — geri alması aylar sürer | ⚠️ **MEKANİK ÇAPASI YOK, SAYILDI.** Yerini tutan şey `deploy/k8s/40-ingress.yaml`'in kendi ölçüm bloğudur — komut **ve** çıktı — bir test değil; manifest bir küme durumunu tarif eder, testler kümeye bakmaz |
| **T32** geliştirme compose'u `log_statement=all` ile koşuyor; bind parametreleri log'a düşüyor (bcrypt digest, `password_resets.token_hash`) | Kapsam **dev/CI**; üretim yönetilen/ayrı Postgres, ve denetçiler bu log'u **ölçüm aracı** olarak kullanıyor | Kapatmak denetim yeteneğini kaldırır; §4.7'ye değen yüzey **repoda yazılı** (`00018`, `00019`, `00021` başlıkları + `deploy/README.md`'nin rotasyon ön koşulu) | `TestStoreParams_AreNeverHandedToAPrintingCall` + `TestStoreParams_ArePrintableAndThatIsTheHazard` — **uygulamanın** sır basmadığını tutar; Postgres'in ne yazdığını bir Go testi tutamaz, ve bu fark burada yazılıdır |
| **T38** düz HTTP'de panele `localhost` dışından girilemiyor (`Sec-Fetch-Site` yok, `Origin: null`) | Üretim **HTTPS**; kapı orada çalışır. Kalan, **TLS öncesi** LAN demosu | Gevşetmek panelin CSRF savunmasını **kalıcı** olarak zayıflatır; ekranın *"süresi doldu"* demesi ayrı ve daha küçük bir kusur (M8-07'ye) | ⚠️ **MEKANİK ÇAPASI YOK, SAYILDI.** Kapı `internal/handler/adminlogin.go`'nun `sameOrigin`'idir ve tarayıcı davranışına bağlıdır; bir Go testi `Sec-Fetch-Site`'ı üretmez, **taklit eder** |
| **T39** düz HTTP'de `getCurrentPosition` **hiç** çalışmıyor → §5'in GPS kanıtı kaybolur | Aynı ön koşul: **TLS**. T38'in kardeşi | Tarayıcının "potansiyel olarak güvenilir kaynak" kuralı; üründe kapatılabilir bir yanı yok | ⚠️ **MEKANİK ÇAPASI YOK, SAYILDI** — aynı sebeple. `web/static/js/tap.js`'in hata dalının **ne dediği** ayrı bir iş ve M8-07'ye ait |
| **T48 (3)** runbook satır sözleşmesi yalnız **sondaki** yorumu görüyor; `OWNER_DSN=… ./scripts/…` biçimindeki bir çağrı çıkarımdan **sessizce** düşüyor | Bugün ihlal **yok**; boşluk gelecekteki bir yazıma dair | Çıkarımı genişletmek **yeni bir ayrıştırıcı** ister; aynı dosya üç turdur "detektörü son kusurun şekline göre yazma" dersini kaydediyor | `TestRunbook_ItsOwnCommandsAreRUNNotSCANNED` — çağrıları **koşturur**; boşluk onun *çıkarım* yarısındadır, *koşturma* yarısında değil |
| **T48 (4)** yapıştırma-tehlike dedektörü zsh'in beş yapısından **ikisini** görüyor; sondaki yorumlar hiç taranmıyor | Bugün metinde **0** ihlal | Denylist'in kendisi sayılmış bir limit ve `scanPasteableComments` bunu **kendi gövdesinde** yazıyor; genişletmek gerçek bir zsh matrisi ister | `TestRunbook_PasteableBlocksCarryNoShellHazardInComments` — dedektörün kendisi; limiti **kendi yorumunda** taşıyor |
| **T48 (5)** sarkan-atıf envanteri bütçeyi elle artırarak **büyüyebiliyor** | Sayının kendisi (**53**) dürüst ve iki yönlü bayatlık kontrolü çalışıyor | Bütçeyi artırmak **incelemede görünür** bir adımdır ve tasarım gereği öyle; `!=` (`>` değil) yüksek-su-seviyesi olmasını zaten engelliyor | `TestEveryNamedTestExists` + `TestRatchetOK` — bütçe `!=` ile tutuluyor, yani **hem büyüme hem küçülme** bir düzenleme ister |
| **T51** logger redaksiyonu **tip düzeyinde** değil; R7b alıcının `log` adında olmasına bağlı | Bugün doğru: **347 logger alanının 347'si de `log`** | Çare **paket çapında bir dönüşüm** — ölçüldü: `internal/handler`'da 22 dosyada 224 çağrı yeri + `internal/domain`'de 46. Bir kartın işi değil | `TestObservability_EveryLoggerCallSiteIsSpelledLog` — **tam olarak o önkoşulu** tutar: bir çağrı yeri `log`'dan farklı yazılırsa kırmızı, ve kabulün dayanağı o tekbiçimliliktir |
| **T52** yerel geliştirme DB'si mutasyon koşularının yazdığı **128 `employee.added`** satırı taşıyor; `audit_log` append-only olduğu için silinemez | Kapsam **yalnız yerel dev**; üretim yolu `detail`'e ne ad ne adres yazıyor | Silmek §4.3'ü ihlal ederdi; tek çıkış `make db-reset` (T22'de kuruldu, artık **çalışıyor**) | `TestAuditLog_IsAppendOnlyForTheApp` — satırların **niye** silinemediğini tutar; kabulün gerekçesi tam olarak budur |
| **T54 (2)** `deploy/README.md`'de hiçbir kapının korumadığı sayılar var | Sayıların **yanlış olması ürünü bozmuyor**; okuru yanıltıyor | Hepsini bağlamak atıf tarayıcısını sayılara genişletmek demek (maliyeti ölçülmedi); bu turda **iki tanesi** bağlandı (`lock_timeout '5s'` ve olay adları) | `TestWriteSQL_CarriesBothPostConditions` (runbook'un `'5s'`'ini iki kopyada tutar) + `TestObservability_AlertSignalNames` (olay adlarını **tam ad** olarak tutar). Kalan sayılar **açıkça bağsızdır** |
| **T56 (1)** `db-reset.sh`'in yerellik sondası, **reddettiği** hedefte boş bir network ve volume yaratıyor | Yıkıcı değil, proje kapsamlı, veriye dokunamaz | Daha ucuz her sonda **daha kötü** ölçüldü (`docker info` bir *iddia*dır ve Docker Desktop'ı reddeder); compose'suz bir dosya sistemi sorusu **başka bir kapı**dır | Metin `scripts/db-reset.sh` başlığına **ölçümüyle** yazıldı; `TestDbReset_KeepsItsStructuralGuards` sondanın kendisini (ve **sırasını**) tutar |

**Bu turda KAPATILANLAR** (kabul değil, kapanış — çapalarıyla):
`TestRotateScript_AlwaysPassesOnErrorStop` (T48-1: tarama dört `psql` çağrısının
üçünü görüyordu) · `TestWriteSQL_CarriesBothPostConditions` (T48-2: `lock_timeout`
**değeri** pinsizdi) · `TestObservability_AlertSignalNames` (T54-1: alt-dize
eşleşmesi, `tap.decision` → `tap.decisionMUT` **geçiyordu**) ·
`TestCarrierScripts_ParseUnderBash` · `TestDbReset_KeepsItsStructuralGuards` ·
`TestDbReset_RefusesADaemonOnAnotherMachine` ·
`TestPgRestoreVerify_KeepsTheTruncateGuardPredicates` ·
`TestComposeFileDeclaresTheProjectName` (T55: iki taşıyıcı script'in arkasında
hiçbir kapı yoktu) · `TestTransactions_ADocketSaysWhoWroteTheNote` +
`TestLedger_ATenantsPolicySentenceIsMarkedAsTheirs` (fiş, **kararı tenant'ın kendi
belgesi verdiğinde** bunu artık söylüyor — ⚠️ *cümleyi* tenant yazmıyor; bunu
`TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs` ve
`TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument` tutuyor).

🔴 **VE ÜÇ SATIRIN MEKANİK ÇAPASI YOK — BU BİR EKSİKLİK OLARAK YAZILDI, GİZLENMEDİ.**
T28, T38 ve T39'un üçü de **tarayıcı ya da küme** davranışına bağlıdır; bir Go
testi ikisini de üretmez, taklit eder, ve taklit eden bir test kabulü **doğrulamaz**.
Üçünün de gerçek kapısı **dağıtımdır** (TLS + ingress) ve dağıtım bir manifest
tarafından tarif edilir, bir test tarafından değil. Bunu *"kapatıldı"* diye yazmak,
bu görevin dört turdur tekrarlayan **"sağlanmayan garantiyi beyan etmek"** kusurunu
bir kez daha işlemek olurdu.

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
- **[M6-11](../plan/m6-dashboard.md) bu ADR'nin ana çıktısıdır:** risk **1–6**'nın
  beşinin tespit sinyali (buddy çiftleri, GPS-only oranı, ctr-boşluk metriği,
  gps-conflict metriği, tek-cihaz/çapraz-lokasyon-yok kalıbı) bu raporda yüzeye
  çıkar. M6-11 kartı A1·A3·A4·Y-D·Y-E sinyallerini üretmekle yükümlüdür; bu ADR o
  yükümlülüğün gerekçesidir.
  🔴 **Risk 7 ve 8 bu cümlenin DIŞINDADIR ve bu bir eksiklik değil, bir olgudur:**
  ikisinin de M6-11'e verecek bir sinyali yok (7'ninki dolaylı, 8'inki hiç yok).
  Encode riskleri panelde değil, **süreçte ve pilot kapısında** karşılanır.
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
