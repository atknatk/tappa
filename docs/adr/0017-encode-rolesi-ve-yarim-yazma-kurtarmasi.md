# ADR 0017 — Encode rölesi, anahtarın hayatı ve yarım-yazma kurtarması

- **Durum:** kabul edildi
- **Tarih:** 2026-08-20
- **Bağlam:** [M8-05](../plan/m8-deploy-pilot.md) FAZ B — plaket encode aracı.
  Bu ADR **turun 1'idir ve kod yazılmadan önce** yazıldı; bugün repoda tek satır
  encode kodu yoktur.
- **İlgili:** [ADR 0003](0003-sdm-modu-ve-anahtar-yonetimi.md) (SDM modu ve anahtar
  yönetimi — özellikle md. 2, 3, 4, 5) ·
  [ADR 0005](0005-kabul-edilen-riskler.md) **risk 7 ve 8** — bu ADR'nin ürettiği
  iki kabul edilen risk oraya **eklendi** (ADR 0005'in *"Append kuralı"*: yeni
  ADR açılmaz, sicil tek yerdedir) · CLAUDE.md §1, §4.5, §4.6, §4.7 ·
  [deploy/README.md](../../deploy/README.md) → *"Plaket encode — boş çipten duvara"*

> 🔴 **ATIF SÖZLEŞMESİ — bu ADR'deki her AN12196 atıfı için geçerlidir.**
> Aksi belirtilmedikçe **AN12196 atıfları rev. 1.8 numaralandırmasıyladır.**
> Bu bir üslup tercihi değil bir tuzak önlemidir: rev. 2.0 (4 Mart 2025) §1'i
> belgenin sonuna taşıdı, bu yüzden §2–§10 arası her üst bölüm **bir aşağı
> kayar** — revizyonsuz bir *"§6"* rev. 2.0'da *"Personalization example"* değil
> **"Special functionalities"** demektir. Tablo numaraları ise **ayrı**
> davranır ve bölüm kaymasından türetilemez.
> Rev. 2.0 karşılıkları için `internal/sun/an12196_kat_test.go`'nun eşleme
> tablosuna bakın — ⚠️ **o tablo EKSİKTİR ve eksik olduğu biliniyor** (dört ayrı
> turda dört kez yanlışlandı; dosyanın kendi başlığı bunu sayıyor). Yani orada
> bulamamak *"böyle bir çift yok"* demek değildir.
> Veri sayfası (**NT4H2421Gx rev. 3.0**) atıflarında böyle bir belirsizlik yok;
> tek revizyon kullanılıyor.

## Neden ayrı bir ADR

ADR 0003 **hangi ayarların** yazılacağını sabitledi; **kimin yazacağını** açık
bıraktı. `deploy/README.md`'nin *"Dört yol — ÖLÇÜLDÜ (2026-08-20). KARAR YİNE
VERİLMEDİ."* alt bölümü dört taşımayı fiyatıyla ve kısıtıyla ölçtü ama —
başlığının dediği gibi — **hiçbirini işaretlemedi**, çünkü dördü de CLAUDE.md §1
gereği kullanıcı onayı isteyen bir bağımlılık getiriyordu.

Kullanıcı **2026-08-20'de B yolunu seçti**: kendi Android uygulamamız, **APDU
rölesi** şeklinde. Bu bir araç tercihi değil; bir **güven sınırının** yerini
değiştiriyor (düz anahtar artık bir operatör makinesinde değil sunucuda yaşıyor)
ve sunucuya **durumlu yeni bir makine** ekliyor. Üçü de ADR'lik karardır.

⚠️ **O alt bölümün başlığı bu ADR'den sonra bayattır.** *"KARAR YİNE VERİLMEDİ"*
2026-08-20 sabahının doğru cümlesiydi; karar aynı gün geldi ve **burada** duruyor.
`deploy/README.md`'deki **beş** ölü cümlenin her birine tarihli bir
*"→ ADR 0017 ile kapandı"* şerhi düşüldü (başlıklar ve ölçüm tarihsel kayıt olarak
**silinmedi** — o dosyanın kendi kuralı); çelişki görürsen **bu dosya** günceldir.

---

## 1. Bağlam — üçü de ölçüldü, hiçbiri burada yeniden ölçülmüyor

**1.1 Neden bir encode aracı gerekiyor.** Q10 (2026-08-18): encode'lu tedarikçiden
plaket **satın alınmaz**, boş çip alınır ve anahtar üretimi + SDM yapılandırması
Tappa tarafında yapılır. Gerekçe `deploy/README.md` → *"Q10 — plaketleri KENDİMİZ
encode ederiz"*: tedarikçi encode ederse park geneli anahtarları **tedarikçi
bilir**, bu da ADR 0003 md. 3'ün reddettiği tek-nokta felaketinin bir kat yukarı
taşınmış hâlidir.

**1.2 Neden "offline script üret, sonra çipe yapıştır" diye bir yol yok.**
`deploy/README.md` → *"🔴 Neden 'offline script üret, sonra çipe yapıştır' DİYE
BİR YOL YOK"*: `AuthenticateEV2First` iki geçişli **canlı** bir el sıkışmadır,
oturum anahtarları çipin o an ürettiği `RndB`'den doğar ve `TI` çipten gelir.
Dolayısıyla bir C-APDU dizisi **önceden hesaplanamaz** ve bu repodaki diğer iki
anahtar aracının (`test/fixtures/seedkeys`, `cmd/rotatekek`) **saf filtre** şekli
bu görevde **uygulanamaz**.

**1.3 Dört yol ölçüldü, kullanıcı B'yi seçti.** Elenen üçünün gerekçesi:
**A** (USB okuyucu + Go) ~€42–48 donanım + bir PC/SC bağımlılığı istiyordu ·
**C** (iOS) $99/yıl + macOS derleme makinesi, üstelik kararı belirleyecek tek
ölçüm (~20 sn'lik **sert** bağlantı sınırının altında tam bir röleli tur yetişiyor
mu) **yapılmadı** · **D** (üçüncü parti araç) ADR 0003 md. 5 ile doğrudan çarpışıyor
— düz anahtar bizim süreçlerimizin dışında üretiliyor ve o cihazda kalıcılaşıyor.
Ayrıntı ve fiyatların her satırı: `deploy/README.md` → *"Dört yol — ÖLÇÜLDÜ"*.

---

## 2. Karar — röle mimarisi

### 2.1 Sınır: hangi bayt hangi makinede üretilir

**Telefon bir APDU rölesidir.** Kripto **sunucuda** koşar.

| Kim | Ne üretir / ne görür |
|---|---|
| **Telefon (Android)** | Çipe **hazır C-APDU baytlarını** verir, **R-APDU baytlarını** geri okur, ikisini HTTPS üzerinden taşır. Hiçbir baytı **üretmez**, hiçbirini **yorumlamaz**. Kripto kütüphanesi taşımaz. |
| **Sunucu (Go)** | `crypto/rand` ile plaket anahtarını ve `RndA`'yı üretir · `E(K,RndB)`'yi çözer · `RndA‖RndB'`'yi kurar ve şifreler · `TI`, `KSesAuthENC`, `KSesAuthMAC` türetir · `CmdCtr`'ı sayar · `ChangeKey` ve `ChangeFileSettings` gövdelerini şifreler ve MAC'ler · yanıt MAC'lerini doğrular · `sun.Wrap` + `sun.Zero` |

**Protokol tarafı bunu engellemiyor:** `ISO/IEC 14443-4`'te `FWT` **PICC'in** yanıt
gecikmesini sınırlar, PCD'nin iki komut arasındaki düşünme süresini **değil** —
araya bir ağ turu koymak standardın zaman bütçesine dokunmaz (`deploy/README.md`
→ *"🔴 APDU RÖLESİ — dört yolun GERÇEK ayırıcısı burasıdır"*).

**Bunun ADR 0003'e etkisi: yok.** Md. 5'in *"düz anahtar bu adımdan sonra hiçbir
yerde kalıcılaşmaz"* şartı ve *"Anahtar hijyeni"* maddesi 1 (`Wrap`+`Zero` **aynı
süreçte**) **tadil edilmeden** sağlanır; hatta daha dar bir garanti çıkar — süreç
artık bir operatör makinesinde değil, sunucudadır.

### 2.2 🔴 Tehdit modeli: telefonun ele geçtiğini varsay

**Ne kaybetmeyiz.** Düz plaket anahtarı **bir anahtar olarak** telefona hiç
gitmez; KEK (`TAPPA_TAG_KEK`) telefona hiç gitmez; başka plaketlerin anahtarları
telefonda hiç bulunmaz (ADR 0003 md. 3 — plaket-başına rastgele, ortak sır yok);
duvarda hizmette olan hiçbir plaket etkilenmez.

**Ne kaybederiz — ve adı: ENCODE OTURUMUNUN APDU DÖKÜMÜ.** Ele geçmiş bir röle,
o oturumda çipe giden ve çipten gelen **her baytı** görür. Bu, "anahtar" değildir
ama **önemsiz de değildir**, ve sebebi bizim tasarımımız değil çipin
kişiselleştirme protokolüdür:

- Boş bir çipte kimlik doğrulama anahtarı (uygulama anahtarı 0) **fabrika
  varsayılanıdır**, yani **herkesçe bilinir**.
- Oturum anahtarları o anahtardan ve `RndA`/`RndB`'den türer; `RndB` çipten
  **o anahtar altında şifreli** gelir, `RndA` da öyle gider (AN12196 rev. 1.8
  §6.6 tablo 14 adım 25–28; rev. 2.0 §5.6 tablo 14).
- Dolayısıyla dökümü gören biri **oturum anahtarlarını yeniden türetebilir** ve
  `ChangeKey` gövdesini **çözebilir** — yani o oturumda yazılan plaket anahtarını
  **öğrenir**.

🔴 **Bunu kapatan bir tasarım yoktur ve arayan zaman kaybeder — ALTI YOL
DENENDİ, ALTISI DA DÜŞTÜ.** Bir sonraki okur bunları tekrar türetmesin diye
sayılıyor:

| # | Deneme | Neden düşüyor |
|---|---|---|
| 1 | *"Önce anahtar 0'ı değiştirelim, sonrası güvenli olur"* | O **ilk** oturum yine fabrika anahtarı altında koşar; yeni anahtar 0 de aynı dökümde sızar |
| 2 | *"Anahtarı iki tarafta türet, telde gönderme"* | `ChangeKey` gövdesi **simetriktir** (`Old ⊕ New ‖ KeyVer ‖ CRC32`); çipte anahtar anlaşması (Diffie-Hellman vb.) **yoktur** |
| 3 | *"Röleyi iki cihaza böl, kimse tam dökümü görmesin"* | `ChangeKey` **tek** bir komuttur; gövdesini gören taraf her şeyi görür |
| 4 | *"Anahtarı çipin kendisi üretsin"* | Veri sayfası §8.2.4.2: anahtarlar **dışarıdan yazılır**, çipte üretim yolu yok |
| 5 | *"Sonra güvenilir bir okuyucuyla yeniden anahtarla"* | Bu **tam olarak** ADR 0003 md. 5'in MVP dışı bıraktığı *yerinde yeniden-anahtarlama*dır (§5.4) |
| 6 | *"İkinci bir kanaldan sonradan doğrula"* | Doğrulama saldırganın anahtarı **zaten aldığı** anda gerçekleşir; tespit, gizliliğin yerine geçmez |

Genel ifade: **halka açık bir anahtarla başlayan bir kanal üzerinden gizlilik
bootstrap edilemez.** Aynı sınır A yolunda (USB okuyucu) da vardır; röle onu
**yaratmaz**, yalnızca dökümün geçtiği yüzeyi genişletir (operatörün tek makinesi
yerine telefon + ağ yolu).

**Bu yüzden kabul edilen sınır şu şekilde yazılır:** encode **kontrollü ortamda**,
**bizim cihazımızla**, plaket **duvara çıkmadan önce** yapılır. ⚠️ **VE BU BİR
MEKANİZMA DEĞİL, BİR TEMENNİDİR — ölçüldü:** bugün ağaçta encode'a özgü cihaz
kimliği, izin listesi, ağ kısıtı ya da kimlik doğrulama **yoktur**; §3.1'in
`RequireAdmin` + `ratelimit` atfı bir **plan**, bir **bağ** değil (§6 md. 10).
🔴 **Patlama yarıçapı "encode penceresi" DEĞİL** — ilk sürüm öyle yazdı ve §5.3
onu **çürüttü**: kurtarma da *fabrika → bizimki* yönünde `ChangeKey` çağırır ve o
oturum, ilk turdan **günler sonra**, muhtemelen **başka bir telefonla**, yine
halka açık anahtar altında koşar. Doğru ifade: yarıçap **o plakete yapılan HER
FABRİKA-ANAHTARLI OTURUMDUR** — ilk encode turu **ve** her kurtarma turu. Yani
*"kontrollü ortam"* yükümlülüğü **kurtarmayı da kapsar**. (Teşhis sondaları
maruz değildir: sonda 2 bizim anahtarımızla koşar, sonda 3 fabrika anahtarıyla
koşar ama **hiçbir gövde göndermez**.) Etkilenen küme yine **o plaketlerdir** ve şüphe hâlinde yol ADR 0003 md. 5'in
normatif yoludur: **`retire + replace`**. Sızmış bir plaket anahtarı tek başına
`ok` üretmez — CLAUDE.md §5'in diğer üç kanıtı (oturum çerezi = kim,
IP = nerede, GPS = yedek nerede) bağlamaya devam eder.

⚠️ **Bu satır `deploy/README.md`'nin dört-yol karşılaştırmasında SAYILMADI.**
Oradaki 🔴 *"Düz anahtar nerede durur?"* satırı B ve C için *"**Sunucuda** (röle).
Telefonda **hiç** durmaz"*, A için *"**Sunucuda** (röle) **veya** operatör
makinesindeki süreçte"* diyor. Üçü de **doğru ama eksik**, çünkü ölçtükleri şey
anahtarın *durduğu yer*, dökümün *görüldüğü yer* değil. Karar değişmiyor (B ile A
bu eksende **aynı sınıfta**, ikisi de kişiselleştirme anını korumaya dayanıyor),
ama sayılmamış bir maliyet sayılmış sayılmaz.

---

## 3. Anahtarın hayatı — nerede doğar, nerede kullanılır, nerede ölür

| Aşama | Nerede | Ne |
|---|---|---|
| **Doğum** | sunucu süreci, bellek | `crypto/rand` ile 16 bayt (ADR 0003 md. 3). Hiçbir şeyden türetilmez. |
| **Kullanım** | sunucu süreci, bellek | `ChangeKey` gövdesinin kurulmasında (`Old ⊕ New ‖ KeyVer ‖ CRC32`) |
| **Sarmalama** | sunucu süreci | `sun.Wrap(kek, uid, key)` → AES-256-GCM, AAD = **ham 7 baytlık UID** (ADR 0003 md. 4) |
| **Kalıcılaşma** | `tags.aes_key_ref` | yalnız **sarmalı** 44 bayt (`nonce(12) ‖ ciphertext(16) ‖ gcm_tag(16)`) |
| **Ölüm** | sunucu süreci | `sun.Zero(...)` — **her düz anahtar için**, aynı süreçte, **§5.1 adım 9'da** (adım 8 sevk edildiğinde **iki** anahtar vardır: `K_SDMFileRead` ve anahtar 0 — §5.1'in `Zero(anahtarlar)` çoğulu bu yüzden) |

🔴 **DÜZ ANAHTARIN GERÇEK ÖMRÜ BİR APDU TURUDUR, VE İLK SÜRÜM BUNU YANLIŞ
YAZMIŞTI.** Burada *"`Zero` — `Wrap`'ın hemen ardından"* diyordu; kendi §5.1'i
bunu **çürütüyor**: `Wrap` **adım 3**, gövdenin kurulduğu `ChangeKey` **adım 6**,
anahtar 0 varyantı **adım 8**. Yani düz anahtar adım 3 ile adım 9 arasında
**altı APDU turu boyunca** sunucu belleğinde yaşamak **zorundadır** — `Wrap`
onu kalıcılaştırır ama **tüketmez**.

⚠️ **"Altı" TÜRETİLMİŞTİR, sayılabilsin diye açıyorum** (bağsız bir sayı bu
belgede kabul edilmez): adım 4 `AuthenticateEV2First` **iki** alışveriştir
(`90 71 …` → `91AF`, sonra `90 AF …` → `9100`; AN12196 rev. 1.8 §6.6 tablo 14
adım 5 ve 14), adım 5 · 6 · 7 · 8 birer alışveriş → **2 + 1 + 1 + 1 + 1 = 6**.
Adım numarası değil, **tur** sayılıyor.

Bunun iki sonucu var ve ikisi de turun 2'sine yükümlülüktür:

1. **Tutucu §4'ün bellek içi oturumudur.** Düz anahtar oturum nesnesinin bir
   alanıdır, yani ömrü **oturumun TTL'i kadardır** — ✅ ve o TTL **karara bağlandı:
   90 sn** (2026-08-21, FAZ B2c-1; §6 md. 7). Bu satır *"bugün karara bağlanmadı"*
   diyordu.
2. **`Zero` tek bir yolda değil, HER çıkışta çağrılmalıdır:** başarı · hata ·
   zaman aşımı · iptal · süreç kapanışı. `Wrap`'ın hemen ardından sıfırlamak
   **mümkün değil**; bunun yerine tek çıkış noktası (`defer`) ve TTL süpürücüsü
   ikisi birden gerekir.

**Asla:** log'a, HTTP yanıtına, hata mesajına, dosyaya, telefona, çıktıya
(CLAUDE.md §4.7). Doğrulama **değer göstermeden** yapılır — uzunluk, satır sayısı,
eşitlik boolean'ı; hex dökümü değil (`deploy/README.md` → *"Anahtar hijyeni —
iddia değil, mekanizma"*).

**Emsal birebir korunuyor:** `test/fixtures/seedkeys` KEK'i ortamdan okur, asla
basmaz, stdout'a yalnız **sarmalı** değeri taşıyan SQL yazar. Encode akışının
sunucu tarafı aynı şekli alır — tek fark, sarmalı değerin stdout'a basılan bir
metin yerine doğrudan bir `INSERT` ile satıra gitmesidir (§5.2).

⚠️ **BURADA BİR CÜMLE VARDI VE KENDİ §2.2'MİZ ONU ÇÜRÜTÜYOR.** *"Süreçten çıkan
tek anahtar-benzeri şey yine sarmalıdır"* yazıyordu; **yanlış**. Doğru ve dar
ifade: **kalıcılaşan** tek anahtar-benzeri şey sarmalıdır. Süreçten **telefona**
çıkan şeyler arasında `ChangeKey`'in şifreli gövdesi de vardır ve §2.2'nin
ölçtüğü gibi, boş çipte o gövdeyi koruyan oturum anahtarları **halka açık**
fabrika anahtarından türer — yani döküm anahtara **eşdeğerdir**. §3'ü tek başına
okuyup §2.2'yi atlayan biri tam tersi sonuca varırdı; ayrım burada yazılı.

### 3.1 🔴 Satırı HANGİ ROL yazar — ve bu YAZILI BİR CÜMLEYE ÇARPIYOR

Encode akışı bir **HTTP uç noktasıdır** (telefon onunla konuşur), yani sunucu
sürecinde koşar. Bu, rol sorusunu **kapatıyor ve seçim bırakmıyor**:
`internal/db`'nin havuzu, bağlandığı rol superuser / `BYPASSRLS` ise **ya da
RLS'li bir tablonun sahibi (veya sahibinin üyesi) ise** açılışta **reddediyor**
(M8-04 FAZ B2 kapısı; gerekçe metni `RoleRiskWhy`). Yani encode akışının sunucu
tarafı **`tappa_app` olarak bağlanmak zorundadır** — `tappa_owner` seçeneği
mimari olarak yok.

⚠️ **NİTELEYİCİ: RED YALNIZ ÜRETİMDEDİR.** Ölçüldü — çağrı
`roleRefusal(facts, cfg.IsProd())` ve fonksiyonun ilk satırı
`if !f.Privileged() || !isProd { return nil }`. Geliştirmede ayrıcalıklı bir DSN
**uyarılır, reddedilmez**. Yani *"mimari olarak başka seçenek yok"* cümlesi
**üretim için** doğrudur; bir geliştirici makinesinde encode akışını owner
bağlantısıyla koşturmak **mümkündür** ve o, sevk edilen davranışı kanıtlamaz.

**`aes_key_ref` GRANT'ının bugünkü durumu (ölçüldü, ikisi ayrı):**

| Fiil | Bugün | Nerede |
|---|---|---|
| `INSERT` (tablo geneli, **tüm sütunlar** — `aes_key_ref` dahil) | ✅ `tappa_app`'e **verili** | 00004: `GRANT SELECT, INSERT, UPDATE ON tags TO tappa_app` |
| `UPDATE` | ⛔ sütun listeli, ve `aes_key_ref` **listede yok** | 00013: `REVOKE UPDATE …` + `GRANT UPDATE (location_id, last_ctr, status, retired_at, replaced_by)` |
| `DELETE` | ⛔ revoke | 00004 (§4.6) |

**Bu, `deploy/README.md` → *"Anahtar hijyeni"* md. 3'ün cümlesini DARALTIYOR.**
Orada *"Uygulama rolü onu yazamaz … Satırı `tappa_owner` yükler"* yazıyor. Ölçüm:
bu **UPDATE için doğru, INSERT için değil**, ve bugüne kadar doğru *görünmesinin*
sebebi tek yükleyicinin (`scripts/seed.sh` + `seedkeys`) owner olarak koşmasıydı
— yapısal bir engel değil, bir **alışkanlık**. Migration 00013 bunu kendi ağzıyla
söylüyor: INSERT bilerek geri alınmadı ve *"the day a loader writes plaques
through the application role, this line is what has to be revisited"*.
**O gün bu ADR'dir.**

**Neden bu kabul edilebilir — üç ölçülmüş sınır:**

1. **§4.7 delinmiyor.** Rol sınırını geçen şey **sarmalı 44 bayttır**, düz
   anahtar değil — `seedkeys` emsalinin tam olarak geçirdiği şey.
2. **§4.5 TENANT EKSENİNDE delinmiyor** — ve niteleyici zorunlu, çünkü ilk
   sürümün geniş hâli yanlıştı. `tags` politikası `USING` **ve** `WITH CHECK`
   taşıyor (00004), `tappa_app` `NOBYPASSRLS` — yani INSERT edilen satır
   **çağıranın tenant'ından başkasına düşemez** (ölçüldü: sahte `tenant_id` ile
   INSERT → *"new row violates row-level security policy"*).
   🔴 **Ama `WITH CHECK` TENANT SAHTECİLİĞİNİ kapatıyor, UID UZAYINI değil** —
   ve o eksen §6 md. 12'de sayılıdır.
3. **Anahtar referansı YAZ-BİR-KEZ olur.** `tappa_app` `aes_key_ref`'i
   yazabilir ama **asla değiştiremez** (sütun listesi). Yanlış bir satır
   düzeltilemez, yalnız `retire + replace` ile emekliye ayrılır — ADR 0003
   md. 5'in normatif yoluyla aynı yön.

⚠️ **Bunun ARDINDAN bir iş doğuyor, artı yarım bir tane** (§6 md. 9 ve md. 10):
`deploy/README.md`'nin o cümlesinin daraltılması **bu turda yapıldı** (md. 9
✅ kapandı; kalan yarısı yalnız *"`aes_key_ref` için sütun düzeyinde bir INSERT
kısıtı gerekir mi"* sorusudur). **Açık olan tek iş** *"kim, hangi tenant için
encode edebilir"* yetkilendirme kapısıdır — bugün böyle **encode'a özgü** bir kapı
**yok** ve `app.tenant_id`'yi kim kuruyorsa satır oraya düşüyor.
⚠️ **Sıfırdan başlanmıyor, ve bunu söylemek gerekiyor:** `internal/httpx` zaten
`RequireAdmin` (`adminidentity.go`) ve bir hız sınırlayıcı (`ratelimit.go`)
taşıyor; turun 2'sinin yapacağı şey yeni bir kimlik yüzeyi icat etmek değil,
**var olanı encode uç noktasına bağlamak** ve *"bu admin hangi tenant için
plaket yükleyebilir"* sorusunu cevaplamaktır.

---

## 4. EV2 oturum durumu **bellekte** yaşar — ve bu bir güvenlik kararıdır

Bir encode turu **en az on** APDU alışverişi sürer ve arada `TI`, `CmdCtr`,
`KSesAuthENC`, `KSesAuthMAC` **ve düz plaket anahtarı** (§3) yaşamak zorundadır.

⚠️ **"En az on" DİZİDEN TÜRETİLDİ, ödünç alınmadı.** İlk sürüm buraya
`deploy/README.md`'nin *"6–17 HTTP gidiş-dönüşü"* aralığını yazmıştı ve **alt
sınırı bu ADR'nin kendi dizisiyle uyuşmuyordu**: o aralık §5.1 yazılmadan önceki
bir kabaca-hesaptır. §5.1'den sayım — ISO SELECT **1** · `GetVersion` **3**
(AN12196 rev. 1.8 §6.5 tablo 13: üç çerçeve) · `AuthenticateEV2First` **2** ·
`WriteData` **1** (NDEF şablonu tek çerçeveye sığmazsa daha fazla) ·
`ChangeKey`×2 **2** · `ChangeFileSettings` **1** → **10**. Üst sınır dizinin
kendisinden değil, NDEF'in kaç çerçeveye bölündüğünden gelir.

İki şık ölçüldü:

| | **Bellek içi** (seçilen) | **Kalıcı** (`encode_sessions` tablosu) |
|---|---|---|
| Anahtar malzemesinin maruziyeti (envanter — 🔴 **DÜZELTİLDİ 2026-08-21, FAZ B2c-1; bu satır `TI` ile `CmdCtr`'ı sayıyordu ve `RndA`/`RndB`'yi hiç saymıyordu, yani iki fazla iki eksikti.** Sevk edilen `keyInventory`: `KSesAuthENC` · `KSesAuthMAC` · **`RndA`** · **`RndB`** · **düz `K_SDMFileRead`** · ve md. 5 kapandığında **düz anahtar 0**. `TI` ve `CmdCtr` **sır değildir** ve bilerek dışarıdadır; `RndA`/`RndB` §9.1.7'ye göre oturum anahtarlarının **bütün girdisidir**. ⚠️ Bugün **bir** düz plaket anahtarı tutuluyor, iki değil — §5.1 adım 9'un `Zero(anahtarlar)` çoğulu **adım 8 sevk edildiğinde** doğru olur) | süreç belleği; süreç ölünce yok. ⚠️ *"Süreç ölünce yok"* bir **gözlemdir, bir sıfırlama değil** — açık `Zero` kuralı §6 md. 7'de | **diskte + her yedekte**. Yedekler `deploy/README.md`'nin KEK bölümündeki *"🔴 Adım 4 — ROTASYON ÖNCESİ YEDEKLER…"* maddesinin gösterdiği gibi ayrı bir maruziyet yüzeyidir |
| Rollout'a dayanır mı | **Hayır** — ölçüldü, aşağıda | Evet |
| Yeni şema borcu | yok | migration + RLS + GRANT + saklama/temizleme politikası (CLAUDE.md §6 beşlisi) |
| Yarım-yazma riski | değişmiyor — §5'teki kurtarma her iki şıkta da **zaten** gerekli | değişmiyor |

**Ölçüm — tek VPS bunu taşır mı:** `deploy/k8s/20-app.yaml` bugün `replicas: 1`
diyor, **ama** `strategy: RollingUpdate` + `maxSurge: 1` + `maxUnavailable: 0`
ile bir rollout sırasında **iki pod aynı anda** ayakta olur ve `deploy/k8s`
altında **hiçbir `sessionAffinity` yoktur** (ölçüldü: `grep` sıfır isabet). Yani
bellek içi bir oturum **rollout penceresinde kopabilir**, ve eski pod'un
`terminationGracePeriodSeconds: 30`'u yarım kalan bir encode turunu
**kurtarmaz**.

🔴 **KOPMA MODLARI İKİYE AYRILIR VE BU ADR'NİN İLK SÜRÜMÜ İKİSİNİ BİRLEŞTİRİP
FAZLA İDDİA ETTİ.** *"Kalıcılık hiçbir turu kurtarmaz"* yazıyordu; **yanlış**, ve
düzeltmesi kararı değiştirmiyor ama gerekçeyi değiştiriyor:

| Kopma | Çipte olan | Kalıcı oturum kurtarır mı |
|---|---|---|
| **Çip tarafı** — telefon alandan çıkar, kullanıcı çipi çeker, çip bir hata döndürür | Çipin kimlik doğrulama durumu **ölür**: *"The authentication state is immediately lost and the error return code is sent without a MAC appended. Note that any other error during the command execution has the same consequences."* (NT4H2421Gx rev. 3.0, s. 28) | 🔴 **Hayır.** Diske yazılmış `TI`/`CmdCtr` işe yaramaz; çip tarafındaki eş yok olmuştur. |
| **Sunucu tarafı** — rollout penceresi, pod ölümü, isteğin öteki pod'a düşmesi | **Hiçbir şey.** Çipe hata dönmez; telefon alanı tutar, `TI`, `CmdCtr` ve kimlik doğrulama durumu çipte **yaşamaya devam eder** | ✅ **Prensipte EVET.** Ve bu, §4'ün **kendi ölçtüğü** tek kopma modudur. |

**Karar yine bellek içi — ama ayakta kalan gerekçelerle, o çürütülmüş cümleyle
değil:**

1. **Oturum anahtarları diske ve HER YEDEĞE taşınır.** `KSesAuthENC` /
   `KSesAuthMAC` kimlik doğrulama anahtarından türer; onları kalıcılaştırmak
   §4.7'nin patlama yarıçapını genişletir ve yedekleri bir maruziyet yüzeyi
   yapar (`deploy/README.md` → *"🔴 Adım 4 — ROTASYON ÖNCESİ YEDEKLER…"*).
2. **Şema borcu gerçek:** CLAUDE.md §6'nın beşlisi (tenant sütunu, RLS, FORCE,
   politika, indeks) + GRANT + saklama/temizleme politikası — bir oturum
   tablosunun **her satırı bir sırdır** ve süresi dolmuş satırların silinmesi
   §4.3 ailesindeki *append-only* alışkanlığıyla ters yönde bir tasarım ister.
3. **Kurtarma yolu ZATEN gerekiyordu.** Çip tarafı kopma kalıcılıkla
   kapatılamıyor, yani §5'in kurtarması her iki şıkta da yazılmak zorunda. Bu
   yüzden kalıcılığın satın aldığı şey **kurtarmanın varlığı** değil, yalnız
   **sunucu kaynaklı kopmalarda bir tur tasarrufu**dur.

⚠️ **SAYILMIŞ LİMİT, kapatılmış değil:** bir rollout penceresine denk gelen encode
turu **kaybolur ve baştan koşar**. Bugünkü ölçekte bu ucuzdur (pilot tek şube,
encode elle ve nadir bir iş) ve rollout **maxUnavailable: 0** sayesinde bir kesinti
değil, yalnızca bir yönlendirme belirsizliğidir. Ucuz olmaktan çıktığı gün
(toplu encode, dakikalar süren oturumlar) doğru cevap bu ADR'yi yeniden açmaktır
— bir oturumu sessizce diske almak değil.

**Bellek içi oturumun TTL'i, eşzamanlılık sınırı ve iptali** turun 2'sinin kabul
kriterleriydi; bu ADR yalnız **nerede yaşadığını** sabitler.
✅ **Üçü de karara bağlandı (2026-08-21, FAZ B2c-1): TTL 90 sn · plaket başına 1,
aktör başına 3, depo geneli 64 · iptal bir ÇIKIŞ YOLUDUR** (oturumu emekli eder,
yalnız isteği reddetmez). Ayrıntı ve gerekçeler §6 md. 7'de.

---

## 5. 🔴 YARIM-YAZMA KURTARMA YOLU

Bir encode turu iki kalıcı yan etki üretir — **çipe yazılan** ve **satıra
yazılan** — ve ikisi de tek başına yarıda kalabilir. Bu bölüm dört soruyu
kapatır: hangi sıra, satır ne zaman, yarım çip nasıl tanınır, kurtarma ADR 0003
md. 5'in hangi tarafındadır.

### 5.0 🔴 ÖNCE İKİ ANAHTAR NUMARASI — SIRA BUNLARA BAĞLI, TERSİ DEĞİL

Bu ADR'nin ilk sürümü sırayı yazıp anahtar numarasını açık bırakmıştı. **Bu bir
çelişkiydi**, çünkü `SDMFileRead` bir **anahtar numarasıdır ve 0 olabilir**
(NT4H2421Gx rev. 3.0 **tablo 11**, s. 13: *"0h..4h — SDMFileReadKey"*; **tablo
14**, s. 14: *"SDMFileReadKey — 00h..04h"*; **tablo 69**, s. 66, bit 11-8:
*"SDMFileRead access right: 0h..4h Targeted AppKey"*). Numara 0 seçilseydi
aşağıdaki adım 6 uygulama master anahtarını değiştirirdi ve adım 7 **ölçülmemiş**
bir oturumda koşardı. Bu yüzden numaralar **burada, bu turda** karara bağlanıyor.

**Karar 1 — `K_SDMFileRead` = uygulama anahtarı `0x01` (sıfırdan farklı).**
Üç gerekçe, üçü de ölçüldü:

1. **Çelişkiyi yapısal olarak kaldırır, ve bu BELGEDEN okunuyor.** AN12196'nın
   kişiselleştirme örneğinde §6.16.1 (`KeyNo ≠ AuthKey`, tablo 26) **anahtar
   0x02'yi** değiştiriyor; hemen ardından §6.16.2 (tablo 27) **anahtar 0x00'ı**
   değiştiriyor. İkisi **aynı oturumdur** ve bu üç değerden okunur: aynı `TI`
   (`7614281A`, §6.10 tablo 20 adım 19'da doğuyor), aynı `KSesAuthENC` ve
   `KSesAuthMAC` (§6.14 tablo 24 adım 23/24'te türetiliyor), ve `CmdCtr`
   `0200` → `0300` diye **yalnızca ilerliyor**. Yani **sıfırdan farklı bir
   anahtarı değiştirmek oturumu DÜŞÜRMEDİ** — belge bunu değerleriyle gösteriyor.
2. **Anahtar 0 kart master anahtarıdır.** Veri sayfası §8.2.4.1: *"A successful
   authentication with the AppMasterKey is required to change any application key
   including the AppMasterKey itself **with the ChangeKey command**."* SDM okuma
   anahtarı olarak kullanmak, iki
   farklı yetkiyi **tek sırra** bağlar: sızan bir plaket anahtarı yalnız SUN
   üretmez, o çipin **NDEF'ini ve tüm anahtarlarını** da açar.
3. **Belgenin vektörleri bu şekille üretilmiş.** AN12196 rev. 1.8 **§6**
   (rev. 2.0: **§5**) *"Personalization example"*'ın örnek yapılandırması *"CMAC-ing key (KSDMFileRead) …: **0x01**"* diyor ve tablo 19
   adım 7'nin `CmdData`'sı `FileAR.SDMFileRead: 0x1` taşıyor. ⚠️ **Dar iddia:**
   `ChangeKey` vektörleri anahtar `0x02` ve `0x00` içindir; `0x01` seçmek onları
   **birebir** eşleştirmez. Eşleşen şey **gövde sınıfıdır** — 1–4 aralığındaki
   her numara aynı 21 baytlık XOR biçimini kullanır (tablo 63).

⚠️ **§6 md. 4'ün ilk sürümü *"hiçbir yerde yazmıyor"* diyordu; bu REPO için
doğruydu, BELGE için değil.** Düzeltildi: belge bir numara **öneriyor** (`0x01`),
repo onu **kararlaştırmamıştı**. Şimdi kararlaştırıldı.

**Karar 2 — uygulama anahtarı 0 DA kişiselleştirilir, ve EN SON adımdır.**
Ölçüldü, çünkü *"fabrika varsayılanında kalsa ne olur"* sorusunun cevabı
belgede yazılı:

- ⚠️ **ÖNCE ATIF, ÇÜNKÜ İLK SÜRÜM BUNU ÖDÜNÇ ALMIŞTI.** Burada
  `AccessRights = 00E0` yazıyordu ve o dize **tüm repoda yalnız bu ADR'de**
  geçiyor: Tappa'nın normatif ayar tablosu (`deploy/README.md` → *"Encode
  ayarları"*) `FileAR.Change`'i ve `FileAR.ReadWrite`'ı **hiç karara
  bağlamıyor**. `00E0`'ın gerçek kaynağı AN12196'nın **örnek** `CmdData`'sıdır
  (rev. 1.8 §6.9 tablo 19 adım 7; rev. 2.0 §5.9 tablo 18) — ve o örnek
  **şifreli-PICC** yapılandırmasıdır, Tappa'nın plain'i değil. Bu satır bu
  yüzden **bir alıntıdır, bir karar değil**. ✅ **Tappa'nın kararı 2026-08-21'de
  verildi ve o tabloya yazıldı** — `Change = Write = ReadWrite = 0x01`,
  `Read = Eh`; gerekçe ve risk 8 etkisi §6 md. 13'te.
- **Sonuç ödünç alınan değere BAĞLI DEĞİL, ve bu ayrı ölçüldü:** veri sayfası
  **tablo 8** (s. 12, *"Default file access rights"*) NDEF dosyasına (`02h`)
  **teslimde** `Change = 0h` veriyor — yani `ChangeFileSettings` daha ilk günden
  anahtar 0'a bağlıdır, biz hiçbir şey yazmasak bile. `Write` ve `ReadWrite` ise
  teslimde `Eh` (serbest) ve **ikisi de** `WriteData`'ya kapı açar (tablo 9), yani
  ikisi birden kilitlenmedikçe yazma kilitli değildir. Aynı tablonun kendi ayak
  notu bunu emrediyor: *"Write and ReadWrite access rights for the NDEF File
  (File No. 02h) **should be changed after personalization** in order to prevent
  unauthorized changes in the NDEF File."*
- Veri sayfası **tablo 9** (s. 12) bu hakları komutlara eşliyor:
  `Change → ChangeFileSettings`, `Write → WriteData / ISOUpdateBinary`.
- Uygulama anahtarlarının teslim değeri **16 bayt `00h`**'dir ve §8.2.4.2 bunu
  açıkça yazıyor: *"The transport value of these 5 keys is 16 bytes of 00h, and
  can be changed by authentication with key number 0 in transport
  configuration."* Aynı madde: *"It is highly recommended to change all 5 keys at
  personalization, even if not all keys are used in the application."*

🔴 **Sonuç — kaybedilen şey adıyla: OLTALAMA.** Anahtar 0 fabrika varsayılanında
kalırsa, **duvardaki plakete fiziksel erişimi olan herkes** (anahtar halka açık)
master olarak kimlik doğrulayıp `WriteData` ile **NDEF URL'sinin host'unu
değiştirebilir** — çalışan plakete dokunur, telefonu **saldırganın sitesini**
açar. Aynı erişim `ChangeFileSettings` ile SDM'i kapatabilir ve `ChangeKey` ile
plaketi hizmet dışı bırakabilir (bu ikincisi **sahtecilik değil**, hizmet
reddidir: çipin anahtarı satırdakiyle uyuşmaz → CMAC düşer → reject).
`deploy/README.md`'nin ayar tablosu *"yazma serbest kalırsa duvardaki plaketin
NDEF'ini herkes değiştirebilir"* diyordu ve **haklıydı** — ama yazmayı **halka
açık bir anahtara** kilitlemek, kapıyı kilitleyip anahtarı paspasın altına
bırakmaktır. Bu yüzden anahtar 0 **kişiselleştirilir**.

**Yeri EN SON, ve sebebi bir ölçüm yokluğu.** ⚠️ İlk sürüm bunu *"belge
§6.16.2'den sonra hiçbir komut göstermiyor"* diye yazdı ve **yanlıştı**: belge
devam ediyor (rev. 1.8 §7 / rev. 2.0 §6 *"Special functionalities"* — Random ID,
Get UID, Failed Auth Counter, TagTamper, SDMReadCtr limiti). Ölçülen ve sonucu
taşıyan doğru cümle şudur: **hiçbiri o oturumu SÜRDÜRMÜYOR.** Rev. 2.0 tablo 27
(*"Enabling Random ID"*) **taze** bir oturumla açılıyor — `CmdCtr = 0000` ve
`TI` artık `7614281A` değil, ve oturum anahtarları da farklı. Yani anahtar 0
değiştikten sonra oturumun ayakta kalıp kalmadığı **belgede hâlâ gösterilmiyor**;
sona koymak soruyu ortadan kaldırır. (Karar 1'in aksine bu bir kanıt değil, bir
**kanıt yokluğuna göre konumlanma**dır.)

### 5.1 Komut sırası — ve sapma NEREDE, nerede DEĞİL

Normatif sıra:

```
1. ISO SELECT (NDEF uygulaması)
2. GetVersion  → 7 baytlık UID          (kimlik doğrulama İSTEMEZ)
3. [sunucu] anahtar üret → Wrap → tags satırını YAZ        (§5.2)
4. AuthenticateEV2First(anahtar 0, FABRİKA varsayılanı)
4b. GetCardUID → UID'yi MÜHÜRLÜ olarak yeniden oku ve adım 3'ün satırıyla karşılaştır
   🔴 EKLENDİ 2026-08-21 (FAZ B2c-1, 2. güvenlik denetimi). Yeri **kasıtlı**: kimlik
   doğrulamadan SONRA (komut `CommMode.Full`, oturum ister) ama **ilk geri
   döndürülemez komuttan ÖNCE**. Gerekçe §6 md. 12'de.
5. WriteData → NDEF URL şablonu (mirror yer tutucuları)
6. ChangeKey(anahtar 0x01 = K_SDMFileRead): fabrika → bizimki   [§6.16.1 sınıfı]
7. ChangeFileSettings: plain SDM aç + yazma iznini kilitle (anahtar 0x01 — §6 md. 13)
8. ChangeKey(anahtar 0x00 = AppMasterKey): fabrika → bizimki    [§6.16.2 sınıfı]
   🔴 NORMATİF, ama BİR ŞEMA KARARINA BAĞLI — aşağıya bak (§6 md. 5)
9. [sunucu] Zero(anahtarlar); satırı "encode edildi" olarak işaretle
```

🔴 **ADIM 8'İN AÇIK BİR ÖN KOŞULU VAR VE BU TURDA KAPATILAMAZ.** Anahtar 0'a
plaket-başına rastgele bir değer yazmak, o değeri **saklamayı** gerektirir; oysa
`tags` **tek bir** `aes_key_ref` sütunu taşıyor (00004: `aes_key_ref bytea NOT
NULL`) ve ADR 0003 md. 4 onu **tam 44 bayta** — yani **tek** bir AES-128
anahtarına — sabitliyor. İkinci bir anahtarı saklamak **yeni bir sütun ve
migration** demektir; park geneli tek bir master anahtar kullanmak ise
**ADR 0003 md. 3'ün doğrudan ihlalidir** ve seçenek değildir. Bu yüzden adım 8
**karar olarak yazılıyor** (gerekçesi yukarıda, oltalama ölçümü) ama **turun
2'sinin ilk işi o şema kararıdır**.
🔴 **VE KAPANANA KADAR GEÇERLİ OLAN ŞEY BİR GÜVENLİK ÇİZGİSİDİR, BİR "DİKKATLİ
OL" DEĞİL:** encode aracının **inşası ve testi** beklemez (tezgâhta, kutuda,
laboratuvarda encode serbesttir), ama **bir plaket anahtar 0 fabrika
varsayılanındayken DUVARA ÇIKAMAZ** — `deploy/README.md` → *"FAZ B'ye
devredilenler"* md. 9 ve M8-06'nın **Q23 pilot kapısı md. 7**. Yani bu bir
**pilot** bloklayıcısıdır, encode bloklayıcısı değil.

🔴 **SAPMA YALNIZ 6 ↔ 7 ARASINDADIR. Anahtar 0 konusunda sapma YOK, UYUM VAR.**
İlk sürüm *"AN12196 bunun tersini yapar"* diyordu ve bu **eksik bir okumaydı**:
belgenin kendi 17 maddelik kişiselleştirme listesi **17. ve son adımı**
`Change Key - ApplicationKey 0x00 (Master Key) [Section 6.16.2]` olarak yazıyor
— yani adım 8'in yeri konusunda belge **bizimle aynı fikirde** ve ADR bunu kendi
lehine kullanmalıydı.

**Gerçek sapma — ve etiketi ilk sürümde DAR yazılmıştı; sonucumuz aslında daha
güçlü.** Belge `ChangeFileSettings`'i (§6.9, liste adım 9) **hiçbir uygulama
anahtarı değişikliğinden önce** koyuyor: 17 maddelik listede `0x01` **hiç**
değiştirilmiyor (16. adım `0x02`'yi, 17. adım `0x00`'ı değiştirir). Yani
*"`K_SDMFileRead` değişikliğinden önce koyuyor"* demek belgeyi olduğundan **dar**
okumaktı. Biz tersini yapıyoruz: SDM'i açan komut, o SDM'in dayandığı anahtar
değiştirildikten **sonra** koşar. Gerekçe **fail-closed**:

- Önce `ChangeFileSettings` gelirse, SDM **`K_SDMFileRead` hâlâ fabrika
  varsayılanındayken açılır** — o an ile `ChangeKey` arasında kesintiye uğrayan
  bir çip, **herkesin bildiği bir anahtarla imzalanmış** geçerli görünüşlü SUN
  URL'leri yaymaya başlar.
- Önce `ChangeKey` gelirse, kesinti hâlinde çipte SDM **kapalı** kalır (veri
  sayfası s. 10: *"At delivery, SDM is disabled."*) — çip statik bir NDEF yayar,
  hiçbir SUN üretmez. **Yanlış yön güvenli yöndür.**
- Sapma serbesttir çünkü adım 5–8'in hepsi **aynı** oturumda, **anahtar 0** ile
  koşar: veri sayfası §10.6.1 *"Authentication with application key number 0 is
  required to change the key"* der, `FileAR.Change` ve `FileAR.Write` da
  **teslimde** anahtar 0'dır (tablo 8/9). ⚠️ *"Teslimde"* niteleyicisi
  2026-08-21'de eklendi: §6 md. 13'ün kararı adım 7'de ikisini de `0x01`'e taşır,
  ama **adım 7'nin kendisi** hâlâ teslim değeriyle, yani anahtar 0 oturumunda
  koşar — dizi değişmiyor. Ve belge kendi sırasını normatif saymaz — kişiselleştirme
  adımları için *"Following steps are optional and used as an example only."* der.

**`ChangeKey`'in iki sınıfı KARIŞTIRILAMAZ** — belge onları bilerek ikiye
ayırmıştır ve gövdeleri **farklı uzunluktadır** (veri sayfası tablo 63, s. 62):

| Sınıf | Belge | Gövde | Bizde |
|---|---|---|---|
| **`KeyNo ≠ AuthKey`** (pratikte: anahtar 1–4) | rev. 1.8 §6.16.1 tablo 26 · rev. 2.0 §5.16 tablo 25 | **21 bayt**: `(NewKey XOR OldKey) ‖ KeyVer ‖ CRC32NK` | **adım 6** |
| **`KeyNo = AuthKey`** (pratikte: anahtar 0) | rev. 1.8 §6.16.2 tablo 27 · rev. 2.0 §5.16 tablo 26 | **17 bayt**: `NewKey ‖ KeyVer` — **XOR yok, CRC yok** | **adım 8** |

⚠️ Belge bu ayrımı *"değiştirilen anahtar = kimlik doğrulanan anahtar mı"* diye
adlandırıyor; veri sayfası tablo 63 aynı ayrımı **anahtar numarasıyla** yazıyor
(*"if key 0 is to be changed"* / *"if key 1 to 4 are to be changed"*). Kimlik
doğrulama **daima** anahtar 0 ile yapıldığı için ikisi aynı kapıya çıkar — ama
kod **numaraya** bakmalıdır, oturuma değil.

### 5.2 `tags` satırı **çipe dokunmadan ÖNCE** yazılır

İki yarım-yazma modu var ve ikisi de adlandırılmalı:

| Mod | Ne olur | Ağırlık |
|---|---|---|
| **Satır var, çip yok** | DB *"bu UID için bu anahtarı bekliyoruz"* der; çipte hâlâ fabrika anahtarı vardır. Bir tap CMAC'te düşer → reject; satır zaten `status='unassigned'`, `location_id` NULL, yani duvarda değil. **Kurtarılabilir:** anahtar sarmalı hâlde elimizdedir, aynı anahtar çipe yeniden sürülür. | ⚠️ envanterde ölü satır |
| **Çip var, satır yok** | Çipte **hiçbir yerde bulunmayan** bir anahtar vardır. O çiple bir daha kimlik doğrulanamaz, anahtarı geri alınamaz, SUN'ı doğrulanamaz. Plaket **kalıcı olarak çöptür**. | 🔴 **anahtar kaybı — §4.6 ihlali** |

**Seçim: satır önce.** Ölçülmüş yapısal gerekçe: `tags.uid` **PRIMARY KEY**'dir
ve UID çipin kendi ürettiği bir değerdir — yani satır **hiçbir zaman** çipe
dokunmadan yazılamaz. Ama `GetVersion` UID'yi **kimlik doğrulamadan** verir —
veri sayfası §10.5.2 (s. 58): *"This command is freely accessible without secure
messaging as soon as the PD is selected and there is no active authentication."*
— dolayısıyla satır **kalıcı hiçbir yan etki üretmeden önce** yazılabilir.
⚠️ **Random ID itirazı düşüyor** ve sebebi ölçüldü: `PICCConfig` bit 1 için veri
sayfası (s. 56) *"Random ID is **disabled at delivery time**"* diyor ve §5.1'in
dizisi `SetConfiguration`'ı **hiç çağırmıyor** — yani boş çipte `GetVersion`'ın
döndürdüğü şey gerçek UID'dir.
🔴 **AMA "TESLİMDE KAPALI" BİR GARANTİ DEĞİL, BİR BAŞLANGIÇ DURUMUDUR — 2026-08-21
ölçümü.** `SetConfiguration` yalnız **AppMasterKey** ister (§10.5.1) ve o anahtar,
md. 5 kapanana kadar **halka açık fabrika varsayılanıdır**; yani plakete fiziksel
erişimi olan biri Random ID'yi **açabilir** ve o çipte `GetVersion` UID alanını
**hepsi sıfır** döndürür (Tablo 58: *"All zero — if configured for RandomID"*).
Sıfır UID'i `tags.uid`'e yazmak **PRIMARY KEY**'i işgal ederdi; `internal/sun`
bu turdan itibaren onu **reddediyor**. Ve bu, md. 12'nin dördüncü sonucundan
**ayrı bir eksendir**: burada yalan söyleyen **çip**, orada **telefon**. (Random ID açık bir çipte doğru komut
`GetCardUID` olurdu ve o **kimlik doğrulama ister** — bu ADR o yapılandırmayı
kurmuyor.) Yani gerçek
seçim *"satır mı çip mi"* değil, *"satır, çipin ilk **geri alınamaz**
komutundan önce mi sonra mı"*dır — ve cevap **önce**, çünkü ikinci mod **anahtar
kaybıdır**.

⚠️ **ATIF DÜZELTİLDİ (kararın kendisi DEĞİL).** İlk sürüm bu kaybı §4.6 *"kayıt
asla kaybolmaz"* maddesine bağlıyordu; **yanlış maddeydi**. §4.6'nın gövdesi
**tap kararları** hakkındadır (*"kanıt yetersizse REJECT edip atma: kaydı yaz,
`verdict='flag'` ver"*) ve bir encode turu bir tap değildir. Kaybolan şey bir
mesai kaydı değil, **boş bir plaket ve geri getirilemez bir anahtardır** — yani
bu bir **envanter** kaybı ve **§4.7** (anahtar yönetimi) meselesidir. Karar
değişmiyor, sadece doğru maddeye bağlanıyor.

**§4.6'nın GERÇEKTEN geçerli olduğu yarı: başarısız tur da bir kayıttır.** Yarıda kalan bir tur
satırı **silmez**. Satır `status='unassigned'` ve `location_id IS NULL` ile durur
(`deploy/README.md` → *"Encode edilen satırın durumu"*), yani hizmette görünmez
ve `internal/sun` akışı onu CLAUDE.md §5 satır 1'den reddeder. **Sessiz temizlik
yoktur.**

⚠️ **Ve burada bir cümle GELECEK ZAMANDA yazılmalıdır, çünkü yolu yok.** Bu ADR'nin
ilk sürümü *"Turun kendisi `audit_log`'a düşer"* diyordu — şimdiki zaman, ve
**bugün yanlış**. Ölçüldü: encode akışının kendisi yok; `audit_log.action`
**serbest metindir** (00005, *"olay sözlüğü domain'de büyür"*), `db/queries/audit.sql`
`RecordAuditEvent`'i taşıyor ama **hiçbir encode olayı yazmıyor**; olay adı, aktör
ve **hangi tenant'a yazılacağı** hiçbir yerde kararlaştırılmadı. Doğru ifade:
**turun kendisi `audit_log`'a DÜŞMELİDİR** ve bunu tanımlamak turun 2'sinin
işidir (§6 md. 8). §4.6 uyumunun bugün ayakta duran yarısı **satırın
silinmemesidir**; olay izi henüz bir yükümlülüktür, bir mekanizma değil.

### 5.3 Yarım kalmış bir çip nasıl tanınır

Çipe **üç sonda** yeter ve üçü de var olan komutlarla yapılır:

1. `GetFileSettings` (NDEF dosyası) → SDM açık mı, erişim hakları ve mirror
   ofsetleri beklenen mi. Kapalıysa **adım 7 hiç koşmamıştır**. (AN12196 bu
   komutu kişiselleştirme dizisinde **kimlik doğrulamadan önce** çağırıyor —
   rev. 1.8 §6.4 tablo 12, s. 26; rev. 2.0 §5.4 tablo **10**, s. 24.)
   ✅ **SEVK EDİLDİ (2026-08-21, FAZ B2b):** `internal/sun/apdu.go` →
   `GetFileSettingsCommand` ve `internal/sun/filesettings.go` → `ParseFileSettings`.
   ⚠️ *"Kimlik doğrulamadan önce"* artık bir çıkarım değil **ölçüm**: veri sayfası
   Tablo 22 bu komutu `CommMode.MAC` diye listeliyor — `GetVersion`'a verdiği
   etiketin aynısı, ve `GetVersion`'ın kendi bölümü onun *"freely accessible
   without secure messaging"* olduğunu yazıyor (§10.5.2). AN12196 soruyu **yaparak**
   kapatıyor: dizide adım **4**, kimlik doğrulaması olan adım **6**'dan önce, ve
   yanıtı düz basılı. Md. 13'ün `Change = 0x01` kararı bu sondayı **bozmuyor**:
   Tablo 9 `Change`'i yalnız `ChangeFileSettings`'e bağlar, okumaya değil.
2. `AuthenticateEV2First(anahtar 0x01, **satırdaki** anahtar)` → **başarılı** ise
   adım 6 koşmuştur; **başarısız** ise çipin `K_SDMFileRead`'i hâlâ fabrika
   varsayılanıdır. (Sıfırdan farklı bir anahtar numarasıyla kimlik doğrulama
   belgede vardır — rev. 1.8 §6.10 tablo 20, s. 35 —
   *"AuthenticateEV2First with key 0x03"*; rev. 2.0 §5.10 tablo **19**, s. 32. Ve veri sayfası §8.2.4.4 bunu genel olarak söylüyor:
   *"As the SDMFileReadKey refers to an AppKey, it is available for
   authentication."*)
3. `AuthenticateEV2First(anahtar 0x00, **fabrika** varsayılanı)` → **başarılı**
   ise adım 8 koşmamıştır. (Adım 8 sevk edildiğinde bu sondanın ikinci yarısı
   *"bizim anahtar 0'ımızla dene"* olur ve o, yukarıdaki şema kararına bağlıdır.)

⚠️ **Üç sonda ÜÇ AYRI OTURUMDUR ve bu bir tercih değil zorunluluktur:** başarısız
bir kimlik doğrulama çipin kimlik doğrulama durumunu düşürür (§4'teki veri sayfası
alıntısı), yani üçünü tek oturumda zincirlemeye çalışmak işlemez.

**Üç sonda ÜÇ durumu ayırır** — ilk sürüm *"dört"* diyordu ve **yanlıştı**:

| Durum | Sonda 1 (SDM) | Sonda 2 (anahtar 0x01) |
|---|---|---|
| adım 6 ve 7 koşmamış | kapalı | başarısız |
| adım 6 koştu, 7 koşmadı | kapalı | başarılı |
| ikisi de koştu | açık, beklenen ayarlar | başarılı |

🔴 **Ayırt EDİLEMEYEN çift, adıyla:** *"hiç dokunulmamış"* ile *"yalnız adım 5
(NDEF şablonu) yazıldı"*. Üç sondanın hiçbiri NDEF dosyasının **içeriğini**
okumuyor. **Kurtarma için zararsızdır** — her iki durumda da akış adım 5'ten
yeniden koşar ve NDEF'i üzerine yazar — ama normatif bir metinde sayı yanlış
olamaz, o yüzden burada sayılıyor. Kapatmak isteyen bir tur dördüncü bir sonda
(NDEF içeriğini serbest okuma) ekleyebilir; bu ADR onu **eklemiyor**, çünkü
serbest okumanın SDM açıkken ne döndürdüğü bu turda **ölçülmedi**.

**Kurtarma daima eksik adımı, satırdaki AYNI anahtarla tamamlar; yeni anahtar
üretmez.**

⚠️ **KURTARMA §2.2'NİN PENCERESİNİ YENİDEN AÇAR — ve bu, §2.2'nin *"o pencere"*
ifadesinin KAPSAMADIĞI bir şeydi.** Ayrım keskin ve ikisi de yazılmalı:

- 🔴 **Maruz:** kurtarmanın *fabrika → bizimki* yönündeki `ChangeKey`'i (§5.4).
  O oturum yine **halka açık fabrika anahtarı 0** altında koşar, üstelik ilk
  turdan **günler sonra** ve muhtemelen **başka bir telefonla**. Yani §2.2'nin
  patlama yarıçapı *"encode penceresi"* değil, **"o plakete yapılan her fabrika-
  anahtarlı oturum"**dur. Kurtarma da kontrollü ortamda yapılmalıdır.
- ✅ **Maruz DEĞİL:** sonda 2 ve 3'ün kimlik doğrulamaları. Sonda 2 **satırdaki
  bizim** anahtarımızla koşar; sonda 3 fabrika anahtarıyla koşar ama **hiçbir
  gövde göndermez** — sızacak bir `ChangeKey` yükü yoktur. Teşhis ucuzdur;
  pahalı olan **yazma**dır.

### 5.4 Kurtarma **"yerinde yeniden-anahtarlama" DEĞİLDİR** — ve ayrım iki bağımsız ölçütle yapılır

ADR 0003 md. 5'te `ChangeKey` hakkında **iki cümle** vardır ve yalnız ikincisini
okuyan biri encode aracını **imkânsız** sanır. Bu ADR ayrımı yazıya geçiriyor,
çünkü bir sonraki okur da aynı tuzağa düşecek:

| ADR 0003 md. 5'in cümlesi | Anlamı | Kapsam |
|---|---|---|
| *"her tag'e Tappa'nın plaket-başına rastgele anahtarı yazılır (K_sdmfileread varsayılandan **değiştirilir**)"* | Bu **tam olarak** `ChangeKey`'dir | ✅ **KAPSAM İÇİ** |
| *"NFC üzerinden **yerinde** yeniden-anahtarlama (**mevcut anahtarla** `ChangeKey`) teknik olarak mümkündür ama **MVP kapsamı dışıdır**"* | Duvardaki bir plaketin anahtarını değiştirmek | 🔴 **KAPSAM DIŞI** — normatif yol `retire + replace` |

**Ayıran iki kelime var, biri değil.** İlki, çoğu okurun yakaladığı **"yerinde"**
— plaket duvarda mı, kutuda mı. İkincisi ADR'nin kendi parantezindedir, daha
keskindir ve genellikle atlanır: **"mevcut anahtarla"**, yani *eski anahtar zaten
Tappa'nınki*. Kurtarma **ikisinin de doğru tarafındadır**:

- **Yer:** kurtarılan plaket `status='unassigned'`, `location_id IS NULL` — hiç
  hizmete girmemiştir. "Yerinde" olan bir plaket değildir; kutudadır.
- **Eski anahtar:** kurtarma yalnız *fabrika → bizimki* yönünde `ChangeKey`
  çağırır. Anahtar zaten bizimkiyse (§5.3 sonda 2 başarılı) kurtarma **hiç `ChangeKey`
  çağırmaz** — eksik olan yalnız `ChangeFileSettings`'tir.

**Sonuç:** kurtarma yolu ADR 0003 md. 5'i **tadil etmez**. Bu, `deploy/README.md`
→ *"Anahtar teslimi ve döndürme"*'nin zaten çizdiği çizginin aynısıdır
(*"Encode aracı `ChangeKey`'i yalnız boş çipi kişiselleştirmek için kullanacaktır…
bu, sahadaki bir plaketi yeniden anahtarlamak ile aynı şey değildir"*); bu ADR
onu **yarım kalmış çip** hâline genişletiyor ve genişletmenin neden sınırın aynı
tarafında kaldığını yukarıdaki iki ölçütle gösteriyor.

🔴 **Ve bir kırmızı çizgi buradan geçer:** kurtarma **asla** *"ne olduğunu
bilmiyorum, yeni bir anahtarla baştan yazayım"* şekline dönüşemez. Eski anahtarı
bilmeyen bir `ChangeKey` **yapılamaz** (protokol gövdesi `Old ⊕ New` ister); satır
önce yazıldığı için eski anahtar daima bilinir ve daima iki değerden biridir:
**fabrika varsayılanı** ya da **satırdaki anahtar**.

---

## 6. Ne KARAR VERİLMEDİ — dürüst sayım

1. **Hiçbir çip encode edilmedi.** Bu ADR'nin tamamı belge ve şema okumasıdır.
   Silikonla hiçbir şey doğrulanmadı; `deploy/README.md`'nin *"FAZ B'ye
   devredilenler"* listesindeki maddelerin hepsi yerinde duruyor — ve liste bu
   turda **sekizden dokuza** çıktı (md. 9: anahtar 0 maruziyeti).
2. **Android tarafı yazılmadı** — tek satır Kotlin/Java yok, Gradle yok, cihazda
   hiçbir şey koşmadı. Röle turunun uçtan uca gecikmesi **ölçülmedi**.
3. **iOS ölçülmedi.** C yolunu tek başına açacak/kapatacak sayı (~20 sn'lik sert
   sınırın altında tam bir röleli tur) hâlâ ölçülmemiştir; B seçildiği için bu
   ADR onu **kapatmıyor, erteliyor**.
4. ✅ **KARARA BAĞLANDI (bu turda), açık DEĞİL: `K_SDMFileRead` = anahtar `0x01`.**
   Bu madde ilk sürümde *"seçilmedi"* diyordu; açık bırakmak §5.1'in sırasını
   kendi içinde çelişkili kılıyordu, o yüzden §5.0'da karara bağlandı. ⚠️ İlk
   sürümün ikinci yarısı da yanlıştı: *"bu repoda hiçbir yerde yazmıyor"* **repo
   için doğru** (ADR 0003 numara vermiyor; `deploy/README.md`'nin ayar tablosunda
   `SDMFileRead` yalnız alıntı içinde geçiyor) ama **belge için yanlış** —
   AN12196 rev. 1.8 §6'nın (rev. 2.0: §5) örnek yapılandırması
   `KSDMFileRead: 0x01` diyor.
   🔴 **VE BU MADDE MD. 6 İLE KENETLİDİR** (ilk sürüm bunu hiç söylemiyordu):
   numara **0** olsaydı gövde 17 baytlık `NewKey ‖ KeyVer` olurdu, **XOR
   bulunmazdı** ve md. 6'nın *"turun 2'sinin en büyük riski"* dediği şey
   **buharlaşırdı**. `0x01` seçildiği için gövde 21 baytlık XOR biçimidir →
   **md. 6 gerçektir ve ayakta durur.**
5. 🔴 **AÇIK, ve turun 2'sinin ÖN KOŞULU: anahtar 0 için ikinci bir anahtarın
   nerede saklanacağı.** §5.0 anahtar 0'ın kişiselleştirilmesini **karara
   bağladı** (gerekçe orada: fabrika varsayılanında kalırsa duvardaki plakete
   fiziksel erişimi olan herkes NDEF URL'sini değiştirip çalışanı başka bir siteye
   yollayabilir). Kapatılamayan yarı **şemadır**: `tags` tek bir `aes_key_ref`
   taşır ve ADR 0003 md. 4 onu **tam 44 bayta**, yani tek bir AES-128 anahtarına
   sabitler. **Üç saklama şıkkı SAYILDI ve hiçbiri SEÇİLMEDİ** — üçü de
   bedeliyle ve **§5.3'ün teşhis sondalarına etkisiyle** birlikte
   `deploy/README.md` → *"FAZ B'ye devredilenler"* md. 9'daki tabloda duruyor
   (yeni sütun + migration · aynı plaket sırrından **KDF** ile türetme · **yaz-ve
   -saklama**). ⚠️ **Tek yerde durmalarının sebebi var:** bu ADR bir karar
   kaydıdır, o liste ise M8-05'in **kapanma koşuludur** — şıkları iki yere
   yazmak, ikisinin sessizce ayrışması demekti. 🔴 Dördüncü bir şık **yoktur**:
   park geneli tek bir master anahtar ADR 0003 md. 3'ün **doğrudan ihlalidir** ve
   seçenek olarak sayılmaz. Kapanana kadar sevk edilen her plaket bu riski
   **bilerek** taşır ve o risk artık aynı listede **9. madde** olarak yazılı —
   ayrıca *"anahtar 0 fabrika varsayılanındayken plaket duvara çıkamaz"* güvenlik
   çizgisiyle birlikte.
   ⚠️ Anahtar **2, 3, 4** için karar yok; veri sayfası §8.2.4.2 hepsini öneriyor
   (*"highly recommended to change all 5 keys at personalization"*) ve aynı
   saklama sorusu onlar için de geçerlidir.
6. 🔴 **`ChangeKey` gövdesinin XOR yarısını doğrulayan bir dış vektör YOK** ve bu,
   turun 2'sinin en büyük tek riskidir — M2-08'in tam olarak yaşandığı sınıf.
   Ölçüldü: veri sayfası (NT4H2421Gx rev. 3.0 §10.6.1 tablo 63, s. 62) anahtar
   1–4 için gövdeyi `(NewKey XOR OldKey) ‖ KeyVer ‖ CRC32NK` diye tanımlıyor;
   AN12196'nın yayımladığı iki `ChangeKey` örneğinin (rev. 1.8 §6.16 tablo
   26/27; rev. 2.0 §5.16 tablo 25/26) **ikisinde de eski anahtar sıfırdır**,
   dolayısıyla `Old ⊕ New == New` — yani **hiçbir yayımlanmış vektör XOR'un
   yapılıp yapılmadığını ayırt edemez**. Üretimde bu zararsızdır (boş çipte eski
   anahtar zaten sıfırdır); **kurtarma** yolunda ise değildir, çünkü orada eski
   anahtar bizimkidir ve sıfır değildir. Aynı tabloların kapattığı bir şey de
   var ve o **kapatılabilir**: `CRC32NK`'nın serileştirmesi. Veri sayfasının
   *"IEEE Std 802.3-2008 (FCS Field)"* ifadesi tek başına en az dört makul bayt
   dizilişine izin verir (ham/tümlenmiş × MSB-first/LSB-first) ve **hangisi
   olduğunu söylemez**; tablo 26 adım 7'nin yayımlanmış `CRC32(NewKey)` değeri
   ise bunlardan **tam birini** seçer. ⚠️ Bu satır bir **türetmedir,
   transkripsiyon değil** (`internal/sun/an12196_kat_test.go`'nun kendi
   `katT5URLCtr` uyarısıyla aynı sınıf): belge değeri yayımlıyor, tarifi
   yayımlamıyor. Turun 2'sinde doğru yol, tarifi metne yazmak değil, **belgenin
   değerini beklenen sabit olarak** almak ve dört dizilişten yalnız birinin onu
   ürettiğini bir vaka tablosuyla göstermektir.
7. ✅ **KAPANDI (2026-08-21, FAZ B2c-1).** Madde açıkken şöyle diyordu: *"Bellek içi
   oturumun TTL'i, eşzamanlılık sınırı, iptali VE SIFIRLAMA KURALI yazılmadı … bugün
   **hiçbiri için** bir `Zero` kuralı yazılı değil."* Beşi de karara bağlandı, ve
   gerekçeleri **kodun içindedir** (`internal/encode/session.go`):
   - **TTL = 90 sn**, iki taraftan da bağlı: taban `len(roundSteps) × exchangeBudget`
     (**tablodan türetiliyor**, tekrar yazılmıyor), tavan **tabanın iki katı**.
     ⚠️ `exchangeBudget = 5 sn` bir **bütçedir, ölçüm değil** ve öyle etiketli — röle
     gecikmesi hâlâ **ölçülmedi** (md. 2).
   - **Süpürme İKİ YOLLU:** tembel süpürme (`checkout`) **terk edilmiş** oturuma
     hiç ulaşmaz; süpürücü **goroutine** tick'lerle çalışır. İkisi de gerekli.
   - 🔴 **SIFIRLAMA GARANTİSİ — ve ifadesi YER SAYMIYOR:** *"kaydolmamış bir tamponu
     **hiçbir şey** görmez; mekanikleşen **tek** şey `Session`'a **yeni alan**
     eklenmesidir."* Mekanik yüzü üç parçadır: `sun.Zero` **yalnız** keyring
     metotlarında · `zeroAll`'ın **tam bir** çağıranı (`retireLocked`) · **hiçbir
     ihraç edilmiş API bir `*Session` alıp vermez**. Davranışsal yüzü çıkış yollarını
     **çağıranın kendi dilim başlıklarıyla** ölçer. ⚠️ **SAYILMIŞ AÇIK:** başka her
     dinlenme yeri (başka tipte alan, paket değişkeni, closure yakalaması, map
     değeri **ve map ANAHTARI**) yakalanmıyor — **map anahtarı Go dizesidir ve
     HİÇ sıfırlanamaz**, yani o şekil için bu garanti **kalıcı olarak yanlıştır**.
   - **Eşzamanlılık: plaket başına 1 · aktör başına 3 · depo geneli 64**, üçü de
     gerekçeli. ⚠️ `actor` çağıranın verdiği bir **dizedir**, yani **kazayı** sınırlar,
     düşmanı değil; gerçek tavan **64**'tür.
   - 🔴 **ENVANTER — VE BU MADDENİN ESKİ HÂLİ İKİ YÖNDEN YANLIŞTI.** Sevk edilen
     `keyInventory` **altı yuvadır**: `KSesAuthENC` · `KSesAuthMAC` · **`RndA`** ·
     **`RndB`** · `K_SDMFileRead` · `K_AppMaster`. Eski metin `TI` ile `CmdCtr`'ı
     **anahtar malzemesi sayıyordu** — kod ikisini de **bilerek dışarıda tutuyor**
     (`TI` bir tekrar/araya-girme koruması, `CmdCtr` bir sayaç; *"silmek, sahip
     olmadıkları bir hassasiyeti iddia etmek olurdu"*) — ve `RndA`/`RndB`'yi **hiç
     saymıyordu**, oysa onlar §9.1.7'ye göre **oturum anahtarlarının bütün
     girdisidir**. **İki fazla, iki eksik.**
     ⚠️ **`K_AppMaster` bugün hiç doldurulmuyor** (md. 5 bloke): sevk edilen kod
     **bir** düz plaket anahtarı tutar, **iki değil** — slot, adım 8 geldiğinde silme
     ve çıkış yollarının **zaten kapsaması** için açık duruyor.
   ⚠️ *"Süreç ölünce bellek gider"* hâlâ yerine geçmez, ve `Close` bunu **bekleyerek**
   çözer (boştakileri hemen siler, uçuştakileri **expire eder**, `CloseGrace` kadar
   bekler); kalan tek artık **hiç dönmeyen bir sahiptir**.
8. **Encode olayının `audit_log` izi tanımlanmadı.** §5.2'nin *"tur `audit_log`'a
   düşer"* cümlesi **gelecek zamandır**: `action` serbest metindir (00005) ve
   bugün hiçbir encode olayı yazılmıyor. Turun 2'si üç şeyi kararlaştırmalı:
   **olay adı** (mevcut sözlükle tutarlı — `tag.retired` kalıbı), **aktör**
   (`actor_id` kim? encode'u tetikleyen operatörün `admin_users.id`'si mi?), ve
   **hangi tenant'a** yazıldığı.
9. ✅ **KAPANDI (bu turda): `deploy/README.md` → *"Anahtar hijyeni"* md. 3.**
   *"Uygulama rolü onu **yazamaz** … Satırı `tappa_owner` yükler"* cümlesi
   §3.1'in ölçümüne göre **UPDATE için doğru, INSERT için yanlıştı**; madde
   *"DEĞİŞTİREMEZ"* olarak daraltıldı ve tarihli bir düzeltme bloğu eklendi.
   **Açık kalan:** migration 00013'ün *"revisit"* uyarısının kendisi — encode
   akışı sevk edildiğinde `aes_key_ref` için **sütun düzeyinde bir INSERT
   kısıtı** gerekip gerekmediği bir sonraki tura kalıyor (bugün gerekli
   **değil**: yaz-bir-kez davranışı UPDATE kısıtından zaten geliyor).
10. **Encode uç noktasının YETKİLENDİRME kapısı yok.** *"Kim, hangi tenant için
    plaket encode edebilir"* hiçbir yerde yazılı değil; §3.1'in ölçümüne göre
    satır **`app.tenant_id`'yi kim kurduysa** oraya düşer. Bu, tenant izolasyonunu
    bozmaz (RLS `WITH CHECK` bağlar) ama **yanlış tenant'a plaket yüklemeyi**
    engelleyen bir şey de yoktur. Turun 2'sinin kabul kriteri.
11. **Q08 hâlâ açık** ve bu ADR onu açmıyor: SUN URL'si domaini taşır, domain
    alınmadı, **yanlış host'la encode edilmiş plaket = sahada plaket değişimi**.
    Kural değişmedi: *Q08 kapanmadan üretim plaketi encode edilmez.*
12. 🔴 **UID UZAYI GLOBALDİR, VE BU ADR ONA YAZAN İLK YOLU YARATIYOR.**
    `tags.uid` **PRIMARY KEY**'dir ve tenant kapsamlı **değildir**; bu **doğru ve
    gereklidir** — bir tap geldiğinde elde yalnız URL'deki uid vardır ve tenant o
    aramanın **sonucudur** (00004'ün kendi yorumu, ADR 0002 md. 7). Global tekil
    PK, çözümlemenin ≤1 satır döndürmesini **yapısal** olarak garanti eder.
    **Yan etkisi üç ölçümdür** (`tappa_app`, `BEGIN … ROLLBACK`):
    - B tenant'ı, A'nın **var olan** uid'siyle INSERT → `23505 duplicate key`,
      oysa **aynı bağlantıda** `SELECT` o uid için **0 satır** döndürüyor →
      **çapraz-tenant varlık kehaneti** (RLS satırı gizliyor, PK varlığını ele
      veriyor).
    - B **taze** bir uid'i işgal ederse A onu **göremez** (0 satır), **yazamaz**
      (23505), **silemez** (`permission denied`), **değiştiremez**.
    - §3.1'in *"yaz-bir-kez"* garantisi — doğru ve ölçülü — zararı **kalıcı**
      yapıyor: temizlik yalnız `tappa_owner` ile **elle**.

    **Bugün ulaşılabilir değil, çünkü uç nokta yok — bu ADR onu yaratıyor.** Ve
    üç şey bir araya geliyor: UID **public**tir (plakette basılı, tap URL'sinde),
    §5.1'de uid **telefondan** gelir (adım 2), ve §2.2 telefonu *"ele geçmiş
    varsay"* der.
    **Azaltmanın şekli — ADLANDIRILDI, YAZILMADI; turun 2'sinin kabul
    kriterleri:** uç noktada yetkilendirme (`internal/httpx` zaten `RequireAdmin`
    ve `ratelimit` taşıyor — md. 10) · oran sınırı · ve **bugün tek temizlik
    yolunun `tappa_owner` ile elle müdahale olduğunun** yazılı olması.

    🔴 **DÖRDÜNCÜ SONUÇ — bu madde onu SAYMIYORDU (güvenlik denetimi, 2026-08-21).**
    Yukarıdaki üç ölçüm *"B tenant'ı A'nın uid'ini işgal eder"* eksenindedir.
    Sayılmayan şey daha basit ve daha ağır: **röle `UID_X` döndürür, çip gerçekte
    `UID_Y`'dir.** O zaman §5.1 adım 3 satırı `UID_X` ile yazar ve `Wrap`'ın AAD'si
    `UID_X` olur (ADR 0003 md. 4), ama adım 6–8 **gerçek çipe** koşar → çip `Y`,
    **hiçbir satırda bulunmayan** bir anahtar taşır. Bu tam olarak §5.2'nin
    🔴 *"çip var, satır yok — plaket kalıcı olarak çöp"* modudur, yani **"satır
    önce" kararının engellemek için var olduğu mod**. §5.2'nin bu karara getirdiği
    tek itiraz **Random ID**'dir ve o bir **çip** özelliğidir; §2.2 ise **telefonu**
    düşman sayar — iki ayrı eksen.
    🔴 **YERLEŞİM DÜZELTİLDİ — 2026-08-21, FAZ B2c-1'in 2. güvenlik denetimi (F1).**
    Yukarıdaki cümle *"adım 6'dan **sonra**"* diyor ve **kapı oraya kondu**; ölçüldü,
    **yanlış yerdi**. Yalancı röle senaryosunda çip, uyumsuzluk yakalanmadan önce
    `8D` (`WriteData`) **ve** `C4` (`ChangeKey`) komutlarını kabul ediyordu — yani
    **kendi UID'siyle hiçbir satırda görünmeyen bir plaket anahtarı taşıyordu**, ki bu
    §5.2'nin 🔴 *"çip var, satır yok"* kalıcı kayıp modudur: kapının **engellemek
    için var olduğu** modun ta kendisi.
    🔴 **Ve yerleşimin TEK gerekçesi zaten AYNI TURDA geri çekilmişti:** *"`K_0x01`
    artık bizimken … yanıt MAC'i röle tarafından üretilemez"*. Gerekçe düştü,
    yerleşim yerinde kaldı.
    **Yeni yer: adım 4'ten (kimlik doğrulama) SONRA, ilk geri döndürülemez komuttan
    ÖNCE** — §5.1'de `4b` olarak yazılı.
    **Tespit gücü DEĞİŞMİYOR, ve bu ölçülebilir:** §5.1'e göre adım 5–8'in hepsi
    **anahtar-0 oturumunda** koşar (adım 6 anahtar 1'i değiştirir, yeniden kimlik
    doğrulamaz; veri sayfası §10.6.1 `ChangeKey` için anahtar 0 ister), yani
    `GetCardUID`'in yanıt MAC'i **her iki yerleşimde de** halka açık fabrika anahtarı
    0'dan türer. Risk 7'nin saldırganı ikisini de forge eder — geç yerleşim **hiçbir
    ek güvence satın almıyordu**.
    **Kazanç tek taraflı:** uyumsuzluk artık yalnız **hayalet envanter satırı**
    bırakıyor (`status='unassigned'`, `location_id IS NULL`, hizmette görünmez) —
    §5.2 asimetrisinin **güvenli yarısı**. **Ek alışveriş maliyeti yok**
    (`GetCardUID` `CommMode.Full`'dur, adım 4'ten itibaren kullanılabilir).

    ⚠️ **VE BU MADDENİN *"çip hurdadır"* SONUCU DA ÖLÇÜLEREK YANLIŞLANDI (F2).**
    Hayalet satırdaki sarmalın AAD'si **RowUID**'dir ve RowUID **halka açıktır**;
    ölçüldü: `sun.Unwrap(kek, RowUID, ref)` **açılıyor** ve çıkan anahtar **çipteki
    anahtarın aynısı**, üstelik anahtar 0 hâlâ fabrika değerinde. Yani çip
    **kurtarılabilir** — satırdan `Unwrap`, aynı anahtarla yeniden sür ya da anahtar 0
    ile yeniden anahtarla. *"Hurda"* demek, gerçek bir Tappa plaket anahtarı taşıyan
    bir çipi çöpe attırırdı.

    **Bu turda yapılan:** `ParseGetVersion` artık belgeye dayanan iki dejenere
    UID'i reddediyor — **hepsi sıfır** (Tablo 58: Random ID durumu) ve **ilk baytı
    `04h` olmayan** (§8.1: *"the first byte of the double size UID is fixed to
    04h"*). ⚠️ **Bu saldırıyı KAPATMAZ ve öyle sunulmuyor:** yalancı bir röle
    **geçerli görünen bir kurban UID'si** döndürebilir ve `GetVersion` yanıtının
    hiçbir yerinde kimlik doğrulama yoktur.
    🔴 **ÇARE — SEVK EDİLDİ (FAZ B2c-1), ve bu paragrafın İLK HÂLİ İKİ AYRI ÇÜRÜTÜLMÜŞ
    YARIYI EMİR KİPİNDE TAŞIYORDU; ikisi de burada, yerinde geri çekiliyor (9. denetim,
    2026-08-21).** Eski metin şuydu: *"Gerçek çare — B2c'nin işi: **adım 6'dan sonra**,
    `K_0x01` artık bizimken, UID'yi o oturumda `GetCardUID` ile yeniden oku (…, yani
    yanıt MAC'i röle tarafından **üretilemez**)."*
    - 🔴 **Yerleşim yarısı ÇÜRÜK:** adım-6-sonrası yuva **ölçülerek ZARARLI** bulundu —
      o noktada çip `8D` (`WriteData`) ve `C4` (`ChangeKey`) komutlarını çoktan kabul
      etmiştir, yani uyumsuzluk **kendi UID'siyle hiçbir satırda olmayan bir anahtar
      taşıyan çip** bırakır: §5.2'nin **kalıcı kayıp** modu, kapının **engellemek için
      var olduğu** mod. Doğru yer **§5.1 adım `4b`**: kimlik doğrulamadan **sonra**,
      **ilk geri döndürülemez komuttan ÖNCE**. Ayrıntı ve gerekçe **40 satır yukarıda**,
      *"YERLEŞİM DÜZELTİLDİ"* bloğunda — o blok *"yukarıdaki cümle"* diyordu ve **bu
      cümleyi kapsamıyordu**.
    - 🔴 **MAC yarısı ÇÜRÜK:** *"röle tarafından üretilemez"* yanlış; oturum **halka
      açık fabrika anahtarı 0** ile kurulur, risk 7'nin kümesi oturum anahtarlarını
      türetip o MAC'i **üretebilir**. Ayrıntı hemen aşağıdaki düzeltme bloğunda.
    - ⚠️ *"B2c'nin işi"* de **bayat**: kapı **sevk edildi**.

    **Ayakta kalan ve hak edilmiş olan:** UID'yi kimlik doğrulanmış oturumda
    `GetCardUID` ile yeniden okumak, **bir dizeyi düzenleyerek yalan söyleyen röleyi**
    yakalar; §2.2'nin EV2 güvenli mesajlaşmasını uygulayan tam düşmanını yakalamaz. Bu
    **yalanı TESPİT eder, satırı önlemez** — ve **adım 4b'de** sessiz kalıcı kaybı
    **hayalet envanter satırına** indirger. Komut kurucusu: `internal/sun/apdu.go` →
    `GetCardUIDCommand`.

    🔴 **DÜZELTME (2026-08-21, FAZ B2c-1 uygulaması sırasında — ÇARE SEVK EDİLDİ,
    AMA YUKARIDAKİ CÜMLE FAZLA GÜÇLÜYDÜ.** *"Yanıt MAC'i röle tarafından
    **üretilemez**"* — **ölçüldü, yanlış**, ve tam olarak bu belgenin kendi
    denetimlerinin üç kez bulduğu sınıf: **hak ettiğinden fazlasını iddia eden bir
    cümle**. Zincir bu ADR'nin kendi maddelerinden çıkıyor: o oturum **adım 4'te
    uygulama anahtarı 0 ile** kurulur ve o anahtar boş çipte **halka açık fabrika
    varsayılanıdır** (veri sayfası §8.2.4.2). §2.2 ile ADR 0005 risk 7 zaten
    söylüyor: dökümü gören taraf `RndA` ve `RndB`'yi geri türetir, dolayısıyla
    `KSesAuthENC` ve `KSesAuthMAC`'i de — yani o oturumdaki **her** MAC'i, bunun
    dahil, **üretebilir**. Kontrolü taze bir `0x01` oturumuna taşımak da
    kurtarmıyor: aynı döküm adım 6'nın `ChangeKey` gövdesini taşır ve aynı gözlemci
    oradan `K_0x01`'i çıkarır (risk 7'nin tanımı).
    **Hak edilen dar cümle:** bu kapı **MALİYETİ YÜKSELTİR, KAPIYI KAPATMAZ** —
    HTTP gövdesindeki bir dizeyi değiştirerek yalan söyleyen bir röleyi
    (hatalı uygulama, yanlış okunan ikinci çip, tek alanı düzenleyen saldırgan)
    **yakalar**, çünkü öyle bir röle uyumlu bir mühürlü yanıt **üretemez**; §2.2'nin
    *"EV2 güvenli mesajlaşmasını uygulayan"* tam düşman rölesine karşı **hiçbir şey
    tespit etmez**. Kod ve test tam olarak bu kadarını iddia ediyor
    (`internal/encode/driver.go` → `acceptGetCardUID`,
    `TestDriver_ARelayThatLiesAboutTheUIDIsCaught`). Karar değişmedi — kapı ucuz ve
    sevk edildi; değişen **cümlenin niteleyicisi**. Aynı fazla-geniş cümle
    `internal/sun/apdu.go` → `GetCardUIDCommand` yorumunda da vardı ve aynı turda
    daraltıldı.

12b. 🔴 **AÇIK, VE BU TURDA (FAZ B2c-1) DOĞDU: ADIM 5'İN CommMode'U ÖLÇÜLMEDİ.**
    §5.1 adım 5, dosya `02h` hâlâ **teslim haklarındayken** (`Write = ReadWrite = Eh`
    — veri sayfası Tablo 8, s. 12) anahtar-0 oturumunda **CommMode.Full**
    `WriteData` gönderiyor. Veri sayfası **§8.2.3.3 (s. 12)**: *"If authenticated and
    the only access conditions satisfied are the free access Eh ones, then the
    **CommMode.Plain** is to be applied"* — ikizi **§8.2.3.5 (s. 13)** aynı şeyi
    *"has to be applied"* diye tekrarlıyor.
    🔴 **VE ÜÇÜNCÜ BİR UYUMLU İFADE VAR; BU MADDE ONU ANMIYORDU** (8. denetim, N6;
    kendi ölçümümle doğrulandı): **Tablo 13, *"Default communication modes per
    file"***, dosya `02h` için **CommMode.Plain** veriyor (dosya `03h` için
    `CommMode.Full`). Yani belge, *"bir gerilim"*in ima ettiğinden **daha TEKDÜZE** —
    üç ayrı yer aynı yöne işaret ediyor. Sonucu **değiştirmiyor** (hangi kipin
    gerçekten dayatıldığına **silikon** karar verecek), ama bu maddeyi okuyan kişi
    belgenin **iki değil üç** kez aynı şeyi söylediğini bilmeli.
    🔴 **VE BU GERİLİM BİR KEZ YANLIŞ KAPATILDI, O YÜZDEN NASIL KAPATILAMAYACAĞI DA
    YAZILI:** FAZ B2c-1'in ilk turu *"AN12196 §5.8.2 Tablo 17 bunu teslim
    haklarında yapıyor"* dedi; **ölçüldü, yanlış** — **§5.4 (s. 24) birebir**
    *"This step does **not** reflect default delivered NTAG 424 DNA configuration of
    NDEF file settings (0000E0EE00010026000CA)"* diyor ve **Tablo 11 (s. 24)** örnek
    çipin `AccessRights = 00E0`, yani `FileAR.Write = **0**` (anahtar 0) olduğunu
    çözüyor. Anahtar tabanlı bir koşul **CommMode.Full'u zaten gerektirir**, yani
    §8.2.3.3'ün şartı o örnekte **hiç tetiklenmiyor**. Konumsal argüman da düşüyor:
    **§5.4, §5.8'den ÖNCE** geliyor.
    ⚠️ **Çipin ne yapacağı da belgede YOK:** her iki bölüm de hangi **kipin**
    geçerli olduğunu söylüyor, çerçevenin **reddedildiğini değil**. Plain uygulayan
    bir çip mühürlü alanı düz veri olarak dosyaya **yazar** — tur yanıt MAC'inde
    düşer, ama çip *"hâlâ boş"* **değildir**.
    **Neden pilot bloklayıcısı değil:** adım 5 her `ChangeKey`'den **önce** koşar,
    yani hiçbir anahtar değişmemiştir ve plaket **kurtarılabilir** (§5.3 kurtarması
    adım 5'ten yeniden koşar ve NDEF'i üzerine yazar — §5.3'ün zaten *"ayırt
    edemediği çift"* olarak saydığı durum). **Yedek belgenin kendisinde:** §5.8.1,
    *"Write NDEF File - using Cmd.ISOUpdateBinary, CommMode.PLAIN"*.
    **FAZ B3'ün ölçeceği:** gerçek bir çip adım 5'i kabul ediyor mu.
    Kod tarafındaki tam kayıt: `internal/encode/driver.go` → `roundSteps` başlığı.

13. ✅ **KAPANDI (2026-08-21, FAZ B2b): `FileAR.Change` = `FileAR.Write` =
    `FileAR.ReadWrite` = anahtar `0x01`; `FileAR.Read` = `Eh`.**
    Madde açıkken şöyle diyordu: §5.1 adım 7 *"yazma iznini kilitle"* diyor ama
    **hangi anahtara** olduğunu söylemiyor, ve `deploy/README.md`'nin normatif
    ayar tablosu bu iki hakkı **hiç** karara bağlamıyor. İki ölçüm aciliyeti
    veriyordu ve ikisi de kararda duruyor: veri sayfası **tablo 9**'a göre
    `Write` **ile** `ReadWrite` **ikisi de** `WriteData`'ya kapı açar → yalnız
    birini kilitlemek yazmayı **kilitlemez**; ve **tablo 8**'e göre ikisi de
    teslimde `Eh` (serbest), `Change` ise `0h`.

    **Kararın dört gerekçesi ölçüldü** (tamamı `internal/sun/filesettings.go` →
    `TappaNDEFSettings`, hücreler `deploy/README.md`'nin ayar tablosunda):
    - `Change`'i teslim değerinde (`0h`) bırakmak öteki ikisini **süs yapardı**:
      halka açık fabrika anahtarı 0 ile kimlik doğrulayan biri
      `ChangeFileSettings` ile `Write`/`ReadWrite`'ı `Eh`'ye geri çevirip yazardı.
    - **Anahtar `0x00` değil**, çünkü anahtar 0 **bugün halka açıktır** — §5.0
      karar 2 onu kişiselleştiriyor ama sevkiyat md. 5'e bloke. Yazmayı ona
      kilitlemek `deploy/README`'nin kendi imgesiyle paspasın altındaki anahtardır.
    - **`Fh` ("asla") değil**, çünkü Q08 açık (md. 11): host kararlaşmadı, yani
      bir plaketin URL'si duvara çıkmadan **yeniden yazılmak** zorunda kalabilir.
      `Fh` her Q08 düzeltmesini plaket değişimine çevirirdi; `Change = Fh` ayrıca
      §5.3 kurtarmasını dondururdu.
    - **§5.1'in sırasıyla uyumlu, ve bu ayrıca ölçüldü:** `Change` teslimde `0h`
      olduğu için **ilk** `ChangeFileSettings` (adım 7) adım 4'ün anahtar-0
      oturumunda koşar; `Change`'i `0x01`'e taşıyan komut **aynı komuttur**, yani
      anahtar 0 gerektiren **son** ayar komutudur. Adım 5'in `WriteData`'sı ondan
      **önce**, teslim `Write = Eh` altında koşmuş olur. Sonraki hiçbir adım bu
      iki hakka ihtiyaç duymaz: adım 8 `ChangeKey`'dir ve `ChangeKey` dosya
      haklarına değil **AppMasterKey**'e bağlıdır (veri sayfası §8.2.4.1).
    - **§5.3'ün üç yarım-yazma durumu da ulaşılabilir kalıyor** (durum durum
      sayıldı, aynı yerde yazılı): adım 6/7 koşmamışsa fabrika anahtarı 0 her şeyi
      açar · adım 6 koşup 7 koşmamışsa `Change` hâlâ `0h`'dır · ikisi de koşmuşsa
      yazma ve yeniden yapılandırma `0x01` ister ve o **satırdadır** (§5.2 satırı
      **önce** yazıyor, tam olarak bunun için).

    ⚠️ **Bir bedeli sayıldı:** *"çip yazıldı, satır kayboldu"* modunda (§5.2 onu
    zaten kalıcı plaket kaybı sayıyor) `Change = 0h` olan bir çip, `SDMFileRead`'i
    hâlâ fabrikada olan başka bir anahtara çevirerek **kurtarılabilirdi**;
    `Change = 0x01` ile o hurda-değeri de gidiyor.

    🔴 **RİSK 8'E ETKİSİ: DARALTIYOR, KAPATMIYOR** — ve ADR 0005 risk 8 bu turda
    buna göre güncellendi (satır **eklenmedi**, sicil sekiz risktir). Fiziksel
    erişimi ve halka açık fabrika anahtarı 0'ı olan biri artık NDEF URL'sini
    **yazamaz** (oltalama vektörü kapandı) ve SDM'i **kapatamaz**; anahtar `0x01`'i
    **öğrenemez ve değiştiremez** (tablo 63 gövdesi `New XOR Old` **ve** `New`
    üzerinde `CRC32` ister — eski anahtarı bilmeden ikisi de hesaplanamaz).
    **Yapabildiği**: anahtar 0'ı ve 2–4'ü (eskileri halka açık sıfır olduğu için)
    üzerine yazmak → **hizmet reddi ve bizim gelecekteki anahtar-0
    kişiselleştirmemizin önden alınması**, sahtecilik değil.
    🔴 **VE BU MADDE *"ÖLÇÜLMEDİ"* DEMİŞTİ; ÖLÇÜLDÜ (2026-08-21, üçüncü göz).**
    `SetConfiguration` veri sayfası §10.5.1'e göre **AppMasterKey** ister, yani
    fabrika anahtarı 0 ile erişilebilir; Tablo 50'nin `05h` seçeneği
    (**"Capability data"** — LRP onun içinde `PDCap2.1` **bit 1**'dir) LRP modunu
    **KALICI olarak** açar (*"This change is permanent, LRP mode cannot be disabled
    afterwards"*) ve LRP'de SDM MAC §9.3.8.2'nin ayrı yoluyla hesaplanır →
    **doğrulayıcımızın asla okuyamayacağı bir plaket**. ⚠️ Tablo 50'nin **beş**
    seçeneği var (`00h`, `04h`, `05h`, `0Ah`, `0Bh`); tamamı ADR 0005 risk 8'de
    sayılı. Rahatlatıcı yarı da ölçüldü: **Random ID plain UID mirror'ı bozmuyor**
    (veri sayfası s. 12) — **ama `GetVersion`'ın UID alanını sıfırlar** (Tablo 58:
    *"All zero — if configured for RandomID"*), ve encode akışı artık o değeri
    **reddediyor** (`internal/sun/apdu.go`). Sınıf değişmiyor — hizmet reddi, çare
    `retire + replace`.
    🔴 **VE BU KARARIN HİÇ KAPSAMADIĞI BİR YOL SAYILDI:** md. 13 yalnız dosya
    `02h`'yi kilitler; tablo 8'e göre dosya `01h` (**Capability Container**)
    teslimde `Write = ReadWrite = 0h`, dosya `03h` ise `Read = 2h`,
    `Write = ReadWrite = 3h`, `Change = 0h`'dır — yani anahtar `0x00` ve
    `0x02`–`0x04` fabrika sıfırında kaldığı sürece **ikisi de herkese
    yazılabilir**. ⚠️ **Ve bu bir türetme değil:** AN12196 rev. 2.0 **§5.14**
    *"By default, CC file has FileAR.ReadWrite set to 00. **Therefore
    Authentication with Key0 needs to be done**."* diyor ve **§5.15 tablo 24**
    anahtar 0 altında CC'ye yazan tam C-APDU'yu basıyor — `E105h`'i adlandıran
    `Proprietary-File_Ctrl_TLV`'yi yazarak. Saldırı üç adımdır (`E105h`'e URL yaz ·
    `03h`'nin `Read`'ini `2h`'den `Eh`'ye taşı — `Change = 0h` olduğu için mümkün ·
    CC'yi `E105h`'e yönlendir) ve dosya `02h`'ye **hiç dokunmaz**.
    🔴 **Geri alınamaz üyesi:** `ChangeFileSettings` ile `01h`/`03h`'nin herhangi
    bir hakkını **`Fh`** (tablo 6: *"no access"*) yapmak **kalıcıdır** — geri açacak
    komut yine `ChangeFileSettings`'tir (tablo 9) ve veri sayfasında **format ya da
    fabrika ayarlarına dönüş komutu yoktur** (tablo 22 taraması: sıfır eşleşme).
    Aynı hamle **boş bir çipte `02h`'nin `Change`'ine** uygulanırsa **adım 7
    kalıcı olarak başarısız olur** — §5.3 sondası 1 bunu görür.
    Kapatan şey **md. 5**'tir, bu madde değil.

14. 🔴 **BU TUR İKİ MEKANİK KORUMA DÜŞÜRDÜ — geri alma değil, SAYILMIŞ KAYIP.**
    İkisi de `deploy/README.md` → *"Anahtar hijyeni"*ndeydi ve **eski hâlleri
    yanlıştı** (§3 ve §2.2 onları çürüttü), ama düzeltmenin bir bedeli var ve
    sayılmadan geçmemeli:
    - **md. 1** eskiden *"aynı süreçte, **art arda**"* diyordu. *"Art arda"*
      **yapısal** bir sınırdı: anahtar iki ifade kadar yaşar ve bu **kodu
      okuyarak** doğrulanır. Yeni hâlde kalan tek şart *"aynı süreç"*; zaman
      sınırı **TTL + `defer` + süpürücü**'ye devredildi.
      ✅ **KAPANDI (2026-08-21, FAZ B2c-1).** Bu madde *"o üçü §6 md. 7'de **hâlâ
      AÇIK**; runbook ölçülebilir bir sınır yerine **yazılmamış bir yükümlülük**
      taşıyor"* diyordu — md. 7 **bu turda kapatıldığı için** bu cümle çürüdü ve
      **yerinde** düzeltiliyor. Yerini alan şey artık nesir değil, **mekanizma**:
      TTL **90 sn** (tabanı `roundSteps`'ten türetilmiş, tavanı iki katı, ikisi de
      test) · `defer` **panik yolunda** · süpürücü **iki yollu** (tembel + goroutine) ·
      **tek çıkış** `retireLocked` (11 çağrı yeri **go/ast envanteriyle** çivili) ·
      ve garanti ifadesi **yerden bağımsız**, **sayılmış açığıyla** birlikte.
      ⚠️ **NİTELEYİCİ, ve md. 4'ün kendi cümlesiyle uyumlu:** mekanizma
      `internal/encode`'da **sevk edildi** ama **hiçbir yerden import edilmiyor**
      (ölçüldü: paket dışı import **0**), yani runbook'un *"araç yazıldığında; bugün
      yok"* parantezi **akışın bütünü için hâlâ geçerlidir**. Kapanan şey
      **yükümlülüğün nesir olması**, akışın sevk edilmiş olması değil.
    - **md. 6** eskiden *"yalnız sarmalı blob **çıktıya** çıkar"* diyordu —
      §4.7 sınırının **en dar** hâli. Yeni hâl yalnız **kalıcılaşanı** bağlıyor
      ve süreçten çıkan başka bir şey için runbook'ta **hiçbir sınır**
      bırakmıyor; tek bağ ADR'nin §2.2'si, yani **başka bir dosyada**.
    🔴 **DURUM (2026-08-21, FAZ B2c-1 bitişinde) — bu paragraf *"Turun 2'si bu ikisini
    … değiştirmeli"* diye **gelecek zamanda** duruyordu; turun 2 **bu görevdir ve
    bitti**, o yüzden ikisi ayrı ayrı kapanıyor:**
    - **md. 1'in yarısı KAPANDI** (yukarıda, mekanizmasıyla).
    - 🔴 **md. 6'nın yarısı AÇIK KALIYOR, ve sayılıyor.** Süreçten çıkanı bağlayan
      **genel** bir mekanik sınır **yok**. Bugün var olan üçü **adlandırılmış
      kanalları** bağlıyor, iddiayı değil: paket **hiç logger içermiyor** (kaynak
      okuyan test) · **hata mesajları** anahtar baytı taşımıyor (test) ·
      `redline-check.sh` **R7** gömülü anahtar dosyası ve sır taşıyan log çağrısı
      arıyor. Ölçüldü: paket bugün **hiçbir şey yazmıyor** (dosya/stdout yazıcısı
      **0 isabet**) — ama bu **bugünkü kodun bir özelliğidir, bir kapı değil**.
      **Kartın devir listesine adıyla girdi.**
    ⚠️ *"Sayılmış bir açık, kapatıldığı iddia edilenden güvenlidir"* — bu yüzden
    md. 6 **kapatılmadı, SAYILDI**.

15. ⚠️ **KAYNAK DOĞRULAMASI ASİMETRİK — sayılıyor, çünkü bir kanıt gücü farkıdır.**
    Bu ADR'nin AN12196 atıfları iki revizyona dayanıyor ve ikisi **eşit ölçüde
    bağımsız doğrulanmadı**: **rev. 2.0** yarısı ve NT4H2421Gx veri sayfası
    **iki ayrı denetçi** tarafından kendi indirmeleriyle yeniden üretildi;
    **rev. 1.8** yarısı ise yalnız **bir** denetçi tarafından (ikincisi belgeyi
    elde edemedi). Rev. 1.8'e özgü satırlar — özellikle §6'nın 17 maddelik
    kişiselleştirme listesi ve §6.14 tablo 24'ün oturum anahtarları — bu yüzden
    **tek gözle** doğrulanmış durumdadır. Değerler rev. 2.0'da da mevcut
    (ölçüldü: aynı `TI`, aynı iki oturum anahtarı, `CmdCtr` `0200`→`0300`), yani
    sonuç ayakta; kaydedilen şey **doğrulama derinliğidir**, bir şüphe değil.

---

## Değerlendirilen alternatifler

| Alternatif | Neden seçilmedi |
|---|---|
| **Kripto telefonda koşsun** (klasik "encode uygulaması") | Düz anahtar sunucudan çıkar ve bir telefonun belleğine/deposuna girer → ADR 0003 md. 5 ve §4.7 ile çarpışır. Röle aynı işi anahtarı hiç göndermeden yapıyor. |
| **Oturum durumu kalıcı bir tabloda** | Oturum anahtarlarını diske ve **her yedeğe** taşır (§4.7 patlama yarıçapı) ve CLAUDE.md §6'nın beşlisi kadar şema borcu getirir. ⚠️ **Bir şeyi gerçekten kurtarırdı** ve bu sayıldı: sunucu kaynaklı kopmaları (rollout penceresi). Çip kaynaklı kopmaları kurtarmaz — orada kimlik doğrulama durumu çipte ölür (veri sayfası, s. 28) — yani kurtarma yolunu **yine de** yazmak gerekirdi. Bugünkü ölçekte kazanç bedeli karşılamıyor; §4'e bakın. |
| **Önce çipi yaz, sonra satırı** | *"Çip var, satır yok"* = anahtar **hiçbir yerde yok** = plaket kalıcı olarak çöp, ve §4.6 ihlali. Ters sıradaki hata modu (*"satır var, çip yok"*) yalnız envanterde ölü bir satırdır ve tamamen kurtarılabilir. |
| **AN12196'nın liste sırasını `K_SDMFileRead` için de izle** (`ChangeFileSettings` §6.9, sonra `ChangeKey` §6.16.1) | SDM'i **`K_SDMFileRead` fabrika varsayılanındayken** açar; o pencerede kesintiye uğrayan çip herkesin bildiği bir anahtarla imzalanmış SUN yayar. Belge kendi adımları için *"used as an example only"* diyor, sıra normatif değil. ⚠️ **Bu satır YALNIZ o çifti kapsar** — anahtar 0'ın yeri konusunda belgeyle **uyuşuyoruz** (listenin 17. ve son adımı). |
| **`K_SDMFileRead` = anahtar `0x00`** (master anahtarı SDM okuma anahtarı olarak kullan) | Adım 6'yı §6.16.2 sınıfına sokar (17 baytlık gövde) ve adım 7'yi, oturumun ayakta kalıp kalmadığı **belgede gösterilmeyen** bir noktadan sonra koşturur. Ayrıca iki farklı yetkiyi tek sırra bağlar: sızan bir plaket anahtarı yalnız SUN üretmez, çipin NDEF'ini ve tüm anahtarlarını da açar (§5.0 karar 1). |
| **Anahtar 0'ı fabrika varsayılanında bırak** | 🔴 Duvardaki plakete fiziksel erişimi olan herkes — anahtar **halka açık** — master olarak kimlik doğrulayıp `WriteData` ile tap URL'sinin host'unu değiştirebilir: çalışanı **saldırganın sitesine** yollayan bir oltalama vektörü (veri sayfası tablo 9 + §8.2.4.2). Yazmayı halka açık bir anahtara kilitlemek, kapıyı kilitleyip anahtarı paspasın altına bırakmaktır. |
| **Kurtarmayı yeni bir anahtarla baştan encode diye tanımla** | Eski anahtar bilinmeden `ChangeKey` **yapılamaz** (gövde `Old ⊕ New` ister). Ayrıca ADR 0003 md. 5'in *"mevcut anahtarla yeniden-anahtarlama"* yasağına dayanan sınırı bulanıklaştırırdı. |
| **Yarım kalan satırı sil, temiz başla** | §4.6: kayıt asla kaybolmaz. Satır `unassigned` olarak kalır; hizmete girmediği için zararı yoktur. (Olay izinin kendisi henüz bir yükümlülüktür — §6 md. 8.) |
| **Encode satırını `tappa_owner` ile yaz** | Encode akışı bir HTTP uç noktasıdır, yani serving sürecinde koşar; `internal/db`'nin havuzu owner/superuser/BYPASSRLS bir DSN'i **açılışta reddediyor** (M8-04 FAZ B2). Aynı süreçte ikinci bir owner havuzu açmak o kapının **varlık sebebini** yok ederdi. §3.1. |

## Sonuçlar

- **Turun 2'si (encode aracı, kod):** sunucu tarafı Go'da, telefon tarafı bir
  APDU borusu. Sunucu `crypto/aes` + `crypto/cipher` ile kalır — **yeni Go
  bağımlılığı yok** (§1). Kullanıcı **Android dil zincirine** onay verdi, Go
  bağımlılığına vermedi.
- **`K_SDMFileRead` = uygulama anahtarı `0x01`** (§5.0 karar 1). Bunun doğrudan
  sonucu: `ChangeKey` gövdesi **21 baytlık XOR biçimidir**, dolayısıyla §6 md. 6'nın
  XOR riski **gerçektir** ve turun 2'sinde ayrıca ele alınmalıdır.
- **Uygulama anahtarı 0 da kişiselleştirilir ve EN SON adımdır** (§5.0 karar 2) —
  ama sevk edilmesi bir **şema kararına** bağlıdır (§6 md. 5); o kapanana kadar
  plaketler bilinen bir oltalama riskiyle çıkar.
- **Komut sırası normatiftir** (§5.1). AN12196'dan sapma **yalnız**
  `ChangeFileSettings` ↔ `K_SDMFileRead` çiftindedir; anahtar 0'ın sona konması
  belgenin **kendi liste sırasıyla aynıdır**.
- **`tags` satırı çipin ilk geri alınamaz komutundan önce yazılır** (§5.2);
  `status='unassigned'`, `location_id` NULL, ve satırı **`tappa_app` yazar**
  (§3.1 — **üretimde** mimari olarak başka seçenek yok; geliştirmede owner DSN'i
  reddedilmez, yalnız uyarılır).
- **Oturum durumu bellek içidir** (§4). Rollout penceresi **sayılmış bir
  limittir**: orada bir tur kaybolur ve kalıcı bir oturum onu kurtarabilirdi —
  bedeli (oturum anahtarlarının diske ve yedeklere taşınması) bugünkü ölçekte
  kazancı aşıyor.
- **ADR 0003 tadil edilmedi.** Md. 5'in iki cümlesi arasındaki sınır §5.4'te
  yazıya geçti; kurtarma o sınırın **kapsam içi** tarafındadır.
- 🔴 **İKİ KABUL EDİLEN RİSK SİCİLE GİRDİ, BU ADR'DE BIRAKILMADI.**
  ADR 0005'in *"Append kuralı"* kabul edilen risklerin **tek ve tam** bir listesi
  olmasını emrediyor; bu ADR'nin ürettiği ikisi oraya **risk 7** (encode
  oturumunun APDU dökümü — §2.2) ve **risk 8** (anahtar 0 fabrika
  varsayılanında kalan plaket — §5.0) olarak yazıldı, tabloya **ve** kendi alt
  bölümlerine. Sicil sekiz riske çıktı.
  ⚠️ **İkisi de o belgenin genel kalıbını bir yerde bozuyor ve bu gizlenmedi:**
  doğrudan **tespit sinyalleri yok**. Risk 8 bu bakımdan risk 6'nın (fiziksel
  plaket devri) **daha kötü kardeşidir** — aynı fiziksel erişim, ama repointlenen
  bir plaketin tap'i bize **hiç ulaşmadığı** için çelişecek bir kayıt bile
  doğmaz.
- **Bu ADR bir uygulama anlatmıyor.** Kod yazıldığında `deploy/README.md` →
  *"Plaket encode"* bölümü onun kabul kriteri olmaya devam eder; bu ADR o
  bölümün **kararlarını** taşır, adımlarını değil.
