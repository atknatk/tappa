# Durum

> **Bu dosya projenin tek canlı durum kaynağıdır.** Her oturumun sonunda
> güncellenir ([README.md](README.md) → oturum protokolü, adım 6.4).
> Görev kartlarına durum işareti konmaz.

**Son güncelleme:** 2026-08-04 (5. oturum — **M6-02 done · M6 2/12**)

> **M6-02 done — 2026-08-04 (`6757537`), üçüncü göz ONAY, 10 tur, 5 RED.** Panel kabuğu: `layout.Panel`,
> `TabBar` + `EmptyState`, üç CSS bileşen ailesi (`.btn`, `.empty-state`, `.tab-bar`) ve **tek bir tablodan**
> `Protect()` içinde mount edilen **beş sekme rotası**. `AdminHome` placeholder'ı gitti. Sekmeler **bilerek boş** —
> M6-03'ten itibaren doldurulacak.
>
> **🔍 KARTI ÖNCE ÖLÇMEK İKİ KEZ KENDİNİ ÖDEDİ.** (1) Docket motifi ve **beş** damga varyantı **zaten sevk
> edilmişti** — ve **M5-06'da değil, M0 iskeletinde** (`7e12f37`); M5-06 damganın **anatomisini** değiştirmiş,
> varlığını değil. Perforasyon görseli **hiç var olmamış**. Yani kartın üç kriteri **karşılanmıştı** ve iş,
> eksik olan **dört bileşendi**. Kartın damga listesi de eksikti (dört sayıyordu, **beş** var). ⚠️ **Yapıcı
> ORKESTRATÖRÜN brief'indeki tarihlemeyi de ölçümle çürüttü** — ve iki ayrı denetçi `git log -S` ile doğruladı.
> (2) **M6-01'in devrettiği borç ölçümle kapandı:** panel bütçeleri *"10 yönetici × 20 görüntüleme × ~10 HTMX
> parçası"* varsayımıyla türetilmişti; gerçek sunucuda **bir sekme görüntülemesi = TAM 1 ücretli istek**
> (bu görev HTMX **getirmiyor**, ve `/static` kapının **dışında** — bütçe harcanmışken 200 döndü). Adres
> tavanının payı **11,5× (3000/260)**, eski öncülün ima ettiği 1,46× değil. **Üç sabit de DEĞİŞMEDİ.**
>
> **🔴 SEVK EDİLMİŞ BİR KONTRAST HATASI BULUNDU VE DÜZELTİLDİ.** `.docket-label` **3,13:1** ile sevk
> ediliyordu (AA 4,5:1), **12 çağrı yerinde** — **çalışanın tap ekranı ve onay ekranı dahil** — ve beş ton
> daha **2,40–4,36:1** arasındaydı. **Kullanıcı kararı (2026-08-04): hepsi düzeltilsin** → `ink/70`
> (en kötü zeminde **5,58:1**). `/60` **ölçülüp reddedildi** (*"düzeltmeyen bir düzeltme"*, dört zeminde de
> kalıyor). Wordmark **WCAG 1.4.3'ün logotype istisnasını kullanabilirdi; REDDEDİLDİ** ve gerekçesi yazıldı.
> ⚠️ **Bağlayıcı zemin porcelain DEĞİL, `green-lite`** (L 0,8229 < 0,8627) — iki tur ve orkestratörün brief'i
> porcelain varsaymıştı çünkü **sayfa zemini** o; **türetilmiş test yakaladı**.
>
> **Ürünün İLK kontrast testi bu görevle geldi** ve üç `TestCompiledCSS_*`'in aksine **CI'da koşuyor**
> (`input.css` + `tailwind.config.js` okuyor, ikisi de commit'li): paleti config'den **yeniden türetiyor**,
> WCAG'i **yeniden hesaplıyor**, **sıfır çağrı yerinde koşmayı reddediyor**, ve **hangi zeminin bağlayıcı
> olduğunu** pinliyor (işaret silinirse de kırmızı: *"Removing the line is not a way to make this pass"*).
>
> **⚠️ BU GÖREVİN İMZA HATASI: SAYI HİJYENİ — BEŞ TURDA BEŞ KEZ.** Ve beşi de aynı kökten: **bir sayının
> ETİKETİ hangi büyüklüğü gösterdiği kontrol edilmeden yazılıyor.** En öğretici üçü: (a) düzyazı *"PORCELAIN
> IS THE BINDING GROUND"* derken **kendi tablosu ve kendi test dosyası** green-lite diyordu — **aynı tur**
> ikisini birden yazmıştı; (b) aynı cümlede **yük bir paydayla (260), pay başka bir paydayla (200)** →
> yazılan 15×, gerçek **11,5×**; (c) *"türetilen çağrı yeri sayısı"* diye basılan sayı **dosya sayısıydı**
> (5), gerçek **12** — ve **aynı satırdaki döküm 12'ye toplanıyordu**, üstelik bu **kanıt diye sunulan
> sayının kendisiydi**. **Sonuncusunu düzeltirken yapıcı kendi çift sayımını (24) kendi mutasyonuyla yakaladı.**
> **Sıradaki:** "ŞU AN" → **M6-03**.

> **M6-01 B fazı done — 2026-08-03 (`4bc2e72`), iki denetçi ONAY, 18 TUR, 8 RED — projenin en uzun görevi
> (M5-06'nın 15 turunu geçti).** Panel girişi uçtan uca: bcrypt (Q03) · admin oturumu · giriş ekranı ·
> "hangi işletme?" seçicisi · oran sınırı · `audit_log`. **Yeni migration YOK** — A fazının şeması gerçekten
> hazırdı. **Beş yükümlülüğün beşi karşılandı ya da dürüstçe limit yazıldı.**
>
> **Bu görevin öğrettiği tek şey var ve on sekiz turun sekizi onu tekrar etti: BİR DÜZELTMENİN KENDİ AĞI
> AYNI TURDA ÖLÇÜLMEZSE, DÜZELTME YAZILMAMIŞ SAYILIR.** Beş koruma sevk edildi ve **beşinin de silinmesi
> suite'i yeşil bıraktı** — `isLookupableEmail` · `sessionGate` · `sameOriginGate` · `meterOnly`'nin
> ücretlendirmesi · ve `CookiePath`/`maxCandidates`'in **totolojik** testleri (beklentiyi sabitin
> **kendisiyle** yazmak). Her biri ayrı bir turda, ayrı bir denetçi tarafından, **mutasyonla** bulundu.
>
> **🔴 En ağır bulgu güvenlik merceğinden geldi — genel üçüncü göz ONAY verdikten SONRA.** Q03'ün
> 72-bayt reddi (ki x/crypto'nun **canlı bir kusurunu** kapatıyor: `CompareHashAndPassword(hash(72×'a'),
> 100×'a')` **nil** döner) bcrypt'i **kısa devre** yaptırıyordu ve kukla yalnız *"hiç aday yok"* dalında
> ödeniyordu → **kayıtlı e-posta 5,53 ms, kayıtsız 295,42 ms = 53×**. Tek istekle, istatistiksiz,
> kimliksiz, ve **sunucuya maliyeti sıfır bcrypt**. 00011 bunu **OBLIGATION 2** olarak yasaklamıştı ve
> üç yerde *"kapalı"* diye yazılıydı. Düzeltme: uzun parola **aynı digest'e karşı** tam maliyeti öder.
> Ve düzeltmenin kendisi bir sonraki turda yakalandı — eklenen üçüncü `-short` skip'i kehanetin **iç
> döngüdeki tek savunmasını** sildi (delik geri açıkken `go test -short ./...` **14/14 yeşil**).
>
> **Üç bulgu daha, üçü de ölçümle:** `GET /admin` **hiç çerezi olmayan** bir çağırana bütçesiz bir
> `SECURITY DEFINER` resolver okuması ödetiyordu (uydurma 43 karakterlik token **1,36 ms** vs bozuk
> **156 µs**, 600 istekte **0×429**) · onu kapatmak için eklediğim flood kapısı **çıkışı reddedip oturumu
> canlı bırakıyordu** (kurbanın adres anahtarını paylaşan biri 200 isteği yakınca `POST /admin/logout` →
> **429**, `Revoke` **0**, ve sunucu tarafı süre olmadığı için o pencerede oturumu bitirecek **hiçbir yol**
> yok) — **bu benim kararımın regresyonuydu**, `tap.go`'nun **ByAddress → Identify → BySession** deseninin
> yalnız ilk aşamasını uygulamıştım · ve `sessionGate` **koruduğu maliyetin yanlış tarafındaydı** (429 alan
> istek resolver okumasını **ve** `TouchAdminSession` UPDATE'ini **zaten ödemiş** oluyordu; ölçüldü:
> reddedilen istekte bile `last_used_at` değişiyor).
>
> **Sayı hijyeni kendi başına bir bulgu sınıfı oldu — ALTI kez.** `make test-short` bandı **üç kez** dar
> yazıldı ve **üç kez** tutmadı; dördüncü denemede format değişti (**gözlenen aralık 51–74 sn** + `make
> test`'in taşıdığı *"gözlem kaydı, hedef değil"* uyarısı). 00011'in en büyük sayısı (`cost-10 ≈ 60–100 ms`)
> **~4× iyimserdi** çünkü sevk edilen digest'ler **cost 12** (367–372 ms) → 500 adaylık şekil 30–50 sn
> değil **~185 sn CPU**; uygulanmış migration değişmez, düzeltme kartta yaşıyor. 15× flood gevşetmesinin
> gerekçesi **yanlış kolda** ölçülmüştü (uydurma token 1,2 ms yerine canlı oturum **5,7 ms**, çünkü o kol
> bir de UPDATE yazıyor) → gerçek maliyet 4,5 sn değil **17,2 sn**.
>
> **On iki limit yazılı, kapatıldığı iddia edilmedi.** En önemlileri: digest-tarafı zamanlama kolu
> (bozuk/boş `password_hash` → **154–198 ns** vs kukla **297,9 ms** = ~10⁶×; **bugün erişilemez** çünkü
> `password_hash` yazan üretim yolu yok, ama **M6-05/M7-04/M7-02'de süresi doluyor** — kural: *`admin_users.
> password_hash` yalnız `adminauth.Hash` çıktısıyla yazılır*) · çıkış **30000** üçüncü-taraf isteğinde
> reddedilebilir (invaryant **zayıfladı**, yazılı) · `adminSessionLimit = 300` **kopyalandı, türetilmedi**
> (**M6-02'nin en acil borcu** — adres tavanından dar). **Sıradaki:** "ŞU AN" → **M6-02**.

> **M6-01 A fazı done — 2026-08-03 (`66d5442`), iki denetçi ONAY, 3 tur.** M6'nın ilk görevi, M5-02'nin
> **A/B kalıbıyla** bölündü (veri katmanı → auth+ekran) çünkü iş bir migration + resolver + kripto bağımlılığı
> + iki ekran + oran sınırı + audit'i **tek commit'e** sığdıracaktı. **Görevin ilk adımı yine kartı ölçmekti**
> ve yine kart eksikti — ama bu kez eksik olan **şemaydı**: 00006 *"resolver YOK: giriş tenant'ı biliyor"*
> varsayıyor, **hiçbir şey tenant'ı kurmuyordu**. Bu bir **kullanıcı kararı** gerektirdi (global çözümleme +
> tenant seçici). **İki bulgu kayda değer:** (1) `citext`'in `=` operatörü `public`'te, sabit `search_path`
> altında **görünmüyor** ve Postgres **hata vermeden** `text=text`'e düşüyor → kimlik doğrulama araması
> sessizce **harfe duyarlı**; (2) parola hash'i **çıplak `string`**'di ve **altı** basma yolu onu verbatim
> sızdırıyordu. **Ve en ağır madde denetimden çıktı:** aday↔parola bağı yazılmamıştı, ve onu azaltmak için
> önerilen DoS çaresi (*"ilk eşleşmede dur"*) yanlış uygulanırsa **tam olarak o atlatmayı** üretiyor.
> **Sıradaki:** "ŞU AN" → **M6-01 B fazı**, beş yükümlülükle.

> **M5-11 done — 2026-08-02 (`1a945fd`), iki denetçi ONAY, 2 tur. M5 KAPANDI.** Bu görev **sevk edilmiş
> kodda bir §5 ihlalini** düzeltti ve kusurun adı **bir cümleydi**: `decide.go` *"birincil koruma çağıranın
> sorgusudur, **ki practice'i dışlar**"* diyordu — **sorgu dışlamıyordu**. Düzeltme **tek satır**
> (`AND NOT t.practice`); yorum-dışı diff **1**. **İki şey bu görevi kayda değer kılıyor.** Birincisi:
> **kartı yazan bendim ve senaryom yanlıştı** — *"yeniden aktivasyon ikinci bir practice verir"* dedim,
> yapıcı ölçümle çürüttü (`isPracticeTap` `LastForPerson == nil` istiyor **ve** `activated_at` `COALESCE`'lu;
> üstelik bu **repoda zaten yazılıydı**). Gerçek erişilebilir yol **geriye tarihli `occurred_at`**, yani
> **M9-01 kuyruğunun ürettiği şekil**. İkincisi: **ADR'nin ilk hâli, düzelttiği hatayı kendi içinde
> tekrar ediyordu** — *"düzeltme yolu mekanizma olarak zaten var … + `audit_log`"* yazıyordu ve güvenlik
> denetçisi ölçtü: **408 manuel satır / 0 `audit_log` satırı / 0 HTTP rotası**. Yani *"bir cümle, sistemin
> vermediği bir şeyi beyan ediyor"* sınıfı **bu oturumun en pahalı sınıfı** olmakla kalmadı, **kendi
> düzeltmesinin içinde de yeniden doğdu**. **Sıradaki:** "ŞU AN" → **M6-01**.

> **M5-10 done — 2026-08-02 (`68acb81`), iki denetçi ONAY, 6 tur, 4 RED.** **Görevin ilk adımı kodu yazmak
> değil, KARTI ÖLÇMEKTİ** — ve kart büyük ölçüde eskimişti: M5-04'ten **önce** yazılmış, oysa M5-04 imzalı
> tap bağlamını getirmiş ve kartın istediği `first_seen_at`'i **MAC'in içinde** sunucu saati olarak zaten
> sağlıyordu. Kartın **migration + retention altyapısı** istediği yerde doğru cevap **13 satırdı**; tablo
> kullanıcı kararıyla **yapılmadı** çünkü ölçüldü ki koruma tarafında **sıfır** ekliyor (pencere `GET`
> anından ölçülüyor ve o anı **saldırgan seçiyor**). **Asıl ders bu değil ama.** Genel üçüncü göz üç tur
> denetledi ve **yalnız cümle hataları** buldu; `tappa-security-auditor` sonra **bu diff'in ürettiği bir
> §5 ihlali** buldu: bandı açmak `sys:tap-freshness`'i (#4) `sys:employee-deactivated`'in (#7) **önüne**
> koydu, yani deaktif bir oturum **3 dakika bekleyerek** güvenlik uyarısını düşürüyordu. Saldırı yok,
> sadece beklemek. **Ve aile taraması ikinci, daha eski bir örnek buldu** (`sys:occurred-at-bound`, tetiği
> daha ucuz: bir form alanı). ⚠️ **Bir kural, yazıldığı hatayı ELEMİYORSA kural değildir:** eski yerleştirme
> kuralı (*"sırayı bozmayan her yere konabilir"*) ölçüldü ve **hatalı sırayı da kabul ediyordu**; yerine
> **yapıyı** kontrol eden invaryant kondu. **Ve sabit listeli test bir AĞ DEĞİL, DEĞİŞİKLİK DEDEKTÖRÜDÜR** —
> yeniden sıralamayı yakalar, **eklemeye karşı çaresizdir**, çünkü kırmızı testin doğal onarımı listeyi
> güncellemektir ve bu **tam olarak yanlış hamledir** (denetçi sahte bir 11. guardrail'le kanıtladı: üç
> paket de yeşil kalıyordu). **Sıradaki:** "ŞU AN" → **M5-11**, M5'in son görevi.

> **M5-09 done — 2026-08-02 (`b0044c5`), iki denetçi ONAY, 3 tur, 1 RED.** Görev iki iş yaptı:
> **bilinen engeli kaldırmak** (seed'in `aes_key_ref`'i 42 baytlık düz ASCII'ydi → her seed'li plaket
> NFC yolunda **500**; zarf operatörün KEK'ine bağlı ve `Wrap` taze nonce çektiği için **SQL literali
> olamaz** → seed iki adımlı oldu) ve **bir günü gerçek HTTP + gerçek Postgres üzerinde üretmek**
> (10 çalışan, 31 kayıt, hepsi karar motorundan — yeni dosyalarda **sıfır** `INSERT INTO transactions`).
> **Görevin şeklini belirleyen şey ADR 0006'ydı:** debounce sunucu saatiyle ölçüldüğü için gün
> **sıkıştırılamıyor** (beklemesiz koşuda 15 kaydın 10'u `sys:person-debounce`, ve §4.6 gereği 15/15
> satır yine yazılıyor). **Motor fixture'a eğilmedi** — gün gerçek zamanda bekledi, ve bunun bedeli
> (62 sn) kullanıcıya sayılarla soruldu → **`make test` tam kaldı, `make test-short` eklendi**.
> **En değerli çıktı bir test değil, bir MOTOR HATASI:** yapıcı `practice` satırının daha eski bir
> **açık girişi maskelediğini** buldu, iki denetçi bunu **düz HTTP'den erişilebilir** olarak doğruladı
> (§5'in yön kuralı sevk edilmiş kodda ihlal ediliyor, **hiçbir sinyal vermeden**) → kullanıcı kararı:
> **M5-11 açıldı**. **Ve iki denetim aracı kendi kapsamlarından yakalandı:** `assertAfterShiftStart`
> **yapısal olarak boştu** (mutasyonla kanıtlandı: geç kalma hesabı tümden ölse bile yeşil kalıyordu),
> `assertTellableApart` **dejenere değer tuzağına** düştü (iki taraf da aynı damgadan türüyordu — tuzağı
> önlemek için yazılmış kontrolün içinde), ve **`make audit`'in `SRC`'si `test/`'i hiç taramıyordu**.
> **Sıradaki:** "ŞU AN" → **M5-10**, sonra **M5-11**.

> **M5-07 done — 2026-08-01 (`e0a5700`), iki denetçi ONAY, 2 tur.** Görevin **yarısı zaten hazırdı**
> (`practice` M4-06'da sunucu türetimli, TRAINING damgası M5-06'da) — işin ilk yarısı bunu **ölçmekti**,
> ve ölçüm **iki maskeli mutant** buldu: `gather`'ın practice guard'ı silinince suite **yeşil** kalıyordu
> (motor tarafı bağımsız kanıtlı olduğu için), ve mapper'ın `Practice` alanı **eşdeğer mutanttı**.
> **Bu, oturumda üçüncü kez:** *bir garanti A paketinde kanıtlanıp B'de tüketiliyorsa, B'nin onu
> KULLANDIĞI ayrıca pinlenmeli.* Tek RED de aynı aileden: bir test yorumu *"slayta eklenen bir link de
> testi kırar"* diyordu, oysa `assertRefs` href **değer** kümesini karşılaştırıyor, **sayısını değil** —
> metinsiz, izinli hedefli üçüncü bir `<a>` (görünmez ikinci dokunma hedefi) suite'i yeşil bırakıyordu.
> **Sıradaki:** "ŞU AN" → **M5-08** (QR kanalı; Q15: IP zorunlu, GPS tek başına yetmez).

> **M5-06 done — 2026-08-01 (`b3fb2b5`), iki denetçi ONAY.** **15 tur, 11 RED — projenin en uzun görevi**, ve
> neredeyse hepsi **tek sınıftan**: *bir cümle ya da bir SAYI, sistemin vermediği bir şeyi beyan ediyor.* İlk iki RED
> ekranın metnindeydi (§4.6: `ignored` *"Your earlier tap stands."* — debounce verdict/kanaldan bağımsız olduğu için
> öncül `flag`/`reject` olabilir; `reject` başlığı *"Not recorded"* — oysa render edilen Result satırın **kanıtı**).
> **Kalan dokuz RED'in hepsi DÜZELTMENİN kendisinde çıktı:** her koruma bir sonraki turda yenildi (elle kurulmuş
> golden → metin düğümü listesi → öznitelik listesi → eleman listesi → referans listesi), ve her seferinde bloklayan
> şey mekanizma değil **onu anlatan cümlenin fazla söylemesi** oldu. **Bunun üzerine 11. turda "kapanış kuralı"
> konuldu: yeni kanal KAPATILMIYOR, dürüstçe LİMİT olarak sayılıyor** — ve iş üç tur sonra bitti. Bugün 8 kanal
> limit olarak yazılı. **Sonraki görevlerde geçerli ders:** bir mekanizmayı tarif ederken *"tamamen/bitmiş/complete"*
> yazmadan önce onu **yenmeye çalış**; yenemediğini de nasıl denediğinle birlikte yaz. **Sıradaki:** "ŞU AN" → **M5-07**.

> **M5-01 done — 2026-07-31, 5. oturum.** `internal/session` teslim edildi (`a71e1b2`), **iki denetçi
> ONAY** (genel üçüncü göz 3. turda + `tappa-security-auditor` kapanış turunda). **Beş tur sürdü ve iki
> RED gördü — ikisi de AYNI SINIFTAN:** *dosya, sağlamadığı bir güvenlik garantisini yorum olarak beyan
> ediyordu.* (1) `Token` unexported alanda `%v/%+v/%#v/slog` ile ham token basıyordu (`fmt`,
> `CanInterface()==false` olunca `Formatter/Stringer/LogValuer`'ı **atlar**) → `struct{ v *string }`.
> (2) `Cookies` sıfır değeri prod'da **`Secure`'suz** çerez yazıyordu (Go'da yasak olan alanı
> *adlandırmaktır*, `T{}` yazmak değil) → kutup çevrildi, `struct{ insecure bool }`. **Ders M5-02…M5-10
> boyunca geçerli:** bir yorum "hiçbir çağıran X yapamaz" diyorsa X **harici paketten denenmiş** olmalı;
> denenmediyse *sınır* olarak yazılır. **Sıradaki:** "ŞU AN" → **M5-02** (davet + aktivasyon). **🔴 M5
> için BLOKLAYAN devir (N5) HÂLÂ AÇIK:** tap.Decide tenant-farkındalıksız → M5-03/M5-05 Input'u
> TagTenantID/SessionTenantID ile besleyip `sys:tenant-mismatch`'i ateşlemeli (çapraz-tenant deliği).
> N1–N5 + ErrUnknownTag "M4/M5'e devralınan"da. Kritik durum sohbette kalmıyor.

---

## ŞU AN

| | |
|---|---|
| **Kilometre taşı** | **M0 + M1 + M2 + M3 + M4 + M5 TAMAM** ✅ 🎉 · **[Tap akışı](m5-tap-akisi.md) 11/11 — çalışanın gördüğü ürünün tamamı bitti.** Davet → aktivasyon → mini tur → **NFC veya QR** → karar → kayıt → onay ekranı, **gerçek HTTP + gerçek Postgres üzerinde bir GÜN** olarak kanıtlı (`make simulate-day`: 10 çalışan, 31 kayıt, hepsi **karar motorundan**), tap sayfası **3 dk'lık tazelik penceresine** bağlı, ve §5'in yön kuralı sevk edilmiş kodda **doğru**. **Sıradaki: M6 — müdürün gördüğü taraf.** |
| **Sıradaki görev** | **M6-03** — Transactions sekmesi. **UI → skill `tappa-brand` ZORUNLU.** M6-02 kabuğu bıraktı: `layout.Panel`, `TabBar`, `EmptyState`, ve `pages.PanelSections` **tek kaynak** (rotalar ondan mount ediliyor, nav ondan render ediliyor → *"linki 404 veren sekme"* üretilemez bir şekil). 🔴 **M6-03'ÜN DEVRALDIĞI ÜÇ ŞEY** (üçü de M6-02'nin kartında ve `adminratelimit.go`'da yazılı): (1) **filtre çubuğu bileşeni** — M6-02'den taşındı (kullanıcı kararı 2026-08-04: filtrelenecek bir şey yokken dürüstçe sevk edilemezdi; üç yolun üçü de — ölü CSS kuralı · işlevsiz kontrol · uydurma *"kayıt yok"* — bu repoda **bulgu** sayılırdı). (2) **HTMX'i M6-03 vendor'lar** (embed, **CDN YOK**) **ve CSP genişlemesini M6-03 gerekçelendirir** — `adminCSP` bugün `default-src 'none'`; `TestPanelScreens_LoadNoScriptAndReachNoThirdParty` **ilk `<script>`'te kırmızıya dönüyor**, yani sessiz miras imkânsız. (3) **BÜTÇEYİ YENİDEN SAY:** M6-02 ölçtü ki bir sekme görüntülemesi **1 ücretli istek** (pay **11,5×**); parçalar gelince çarpan geri gelir. Eşik yazılı: `adminSessionLimit=300`, **≥15 istek/görüntülemede** bağlayıcı olur. ⚠️ Ayrıca `store.AdminUser.PasswordHash` (sqlc **üretimi**) hâlâ çıplak `string`; **bugün üretimde sıfır referansı var** ama onu döndüren bir sorgu yazılırsa `%+v` sızdırır — koruma **tip düzeyinde değil, yokluk düzeyinde**. |
| **Çalışma modu** | Orkestrasyon + üçüncü göz — [README.md](README.md) · brief'ler [agent-brief.md](agent-brief.md) |
| **Dal** | **`main`** — M0 (`m0-bootstrap`) `main`'e fast-forward birleştirildi (`562f021`), dal silindi. **Kullanıcı kararı (2026-07-25): artık doğrudan `main`'de çalışılır, görev başına dal açılmaz** (CLAUDE.md §10 güncellendi). Push/PR yine istemedikçe yok. |
| **Blokeler** | Yok. **Bekleyen kullanıcı eylemleri → [docs/backlog.md](../backlog.md)** (B1 iPhone/Q11 ölçümü, B2 arm64 Go kurulumu) — **ikisi de hiçbir şeyi bloklamıyor**. Q02 (davet kanalı) M5-02'yi bloklamaz; kart cevapsız hâli için yol gösteriyor. |

**Bir sonraki oturum ne yapmalı:** **M2-02** … **[TAMAMLANDI — M2 kapandı, aşağıki "ŞU AN" M3-02'yi gösterir]**.
M3 sırası: M3-02 (şema) → M3-03 (belge modeli + doğrulama) → M3-04 (değerlendirici) → **M3-05
(guardrail'ler, §4 en kritik)** → M3-06 (baseline) → M3-07 (kararın kayda bağlanması) → M3-08
(gevşetilemezlik kanıtı, kapsam %90+) → M3-09 (ADR 0005 kabul edilen riskler).

### M3-04'e devralınan (M3-01 denetiminden, bloklamayan)
1. **M3-04 kartındaki `Decision` struct yorumu** ([m3-policy-motoru.md](m3-policy-motoru.md) ~satır 251)
   effect'leri `allow | review | deny | ignore` sayıp **`redirect`'i atlıyor** — oysa değerlendirici
   `redirect` de döndürür (`sys:no-session`, `sys:tenant-mismatch`). Kartın önceden var olan küçük
   hatası, M3-01 kapsamı dışıydı; **M3-04 yapılırken kart düzeltilmeli** (agent-brief madde 6).
2. ADR 0004, **değerlendirme anındaki** bilinmeyen operatör/anahtar (sürüm geri-alma sonrası) davranışını
   açıkça yazmıyor; M3-04 kartı yazıyor (ifade **eşleşmez**, koşul atlanmaz — yoksa deny koşulsuzlaşır).
   ADR bununla çelişmiyor ("sessizce yok sayma yok / kısıtlayıcıya düş" doğru yönde). M3-04'te uygula.

### M3-05'e devralınan (M3-03 denetiminden, bloklamayan)
- **Bounded-param üretimde BOŞ.** `internal/policy/validate.go` bounded-param mekanizmasını kurdu ve test
  etti (enjekte edilen aralık → aralık-dışı reddedilir), ama `DefaultLimits().BoundedParams = nil` →
  üretim yolunda ADR §11 koruması **fiilen yok** (değerlendirme henüz olmadığından M3-03/M3-04'te açık
  yaratmaz). **M3-05 doldurmalı.** Denetçi düzeltmesi: eşlenebilir anahtar **ÜÇ** (`tap:gpsDistanceM`,
  `tap:pageAgeSeconds`, **`tap:occurredAtSkewSeconds`** — ADR §11 occurred_at sapması 0–72 sa) + debounce
  (bağlam anahtarı YOK, yalnız config/guardrail param). M3-05 üçünü de + config sınırlarını (GPS 25–1000 m,
  tazelik 1–15 dk, sapma 0–72 sa, debounce 30–300 sn) doldurmazsa koruma **sessizce eksik** kalır.
- **(M3-04 denetiminden)** `internal/policy/evaluate.go:169` bir yorumda kartı alıntılayan **Türkçe** ifade var
  (§7 gri alan: yorum, identifier/log/hata/commit sayılmaz → bloklamadı). internal/policy'ye bir sonraki
  dokunuşta (M3-05/M3-08) İngilizce'ye çevir.

### M3-07'ye devralınan (M3-04 denetiminden, bloklamayan)
- **Default kararı `Layer=guardrail` taşıyor**, `MatchedSid="default"` ile ayrılıyor (kodda gerekçeli — dördüncü
  Layer değeri uydurulmadı). M3-07 raporlama/kayıt yolunda guardrail'i default'tan **`matched_sid`** ile ayırmalı
  (guardrail kararında `policy_version_id` boş + `matched_sid="sys:…"`; default'ta `matched_sid="default"`).

### M4/M5'e devralınan (M3-05 denetiminden — guardrail'lerin girdi sözleşmesi)
Guardrail'ler saf `policy.Evaluate` girdisine güvenir; bu girdiyi M4 (`tap.Decide` bağlam kurar) / M5 (handler)
DOLDURUR. Aşağıdakiler doldurulmazsa guardrail **sessizce** ateşlemez (eksik anahtar ≠ false, M3-04 invariant'ı):
- **N1 — `tap:sunValid`:** M5 her NFC tap'inde bunu set etmeli, yoksa `sys:sun-invalid` sessiz kalır (asıl atomik
  ctr koruması `internal/sun` M2-06'da; guardrail onun policy-katmanı yansıması — ikisi birlikte).
- **N2 — `tap:channel` SUNUCU-türetimi:** `channel` `ctr`/`cmac` varlığından türetilmeli (istemci beyanından
  DEĞİL — ADR 0004 §8). sun-invalid/freshness'in "NFC-only" kapsaması buna dayanır; istemci `channel=qr`
  diyip SUN korumasını atlayamamalı.
- **N3 — debounce değer akışı:** `TAPPA_DEBOUNCE_SECONDS` aralık-kontrollü (M3-05) ama henüz `policy.Params`'a
  **bağlanmadı** (`DefaultParams` debounce=60 sn sabit). M4/M5 config değerini Params'a bağlamalı; bağlanana
  kadar küçük drift riski (bloklamıyor — sınırlar ortak).
- Ayrıca (M2'den): `sun.Verify` `ErrUnknownTag` döndürür → M4/M5 bunu yutmamalı, global güvenlik olayı loglamalı.
- **N4 (M3-07 denetiminden) — M5-05 yazma yolu Decision→sütun sadakati:** `transactions_policy_decision_consistent`
  CHECK + composite FK §4.6'yı ancak M5-05 `policy.Decision`'ı sütunlara sadık eşlerse korur. Bir çağıran
  baseline/tenant için `Policy.VersionID`'yi `uuid.Nil` ile yüklerse: pointer non-nil olur → CHECK branch (c)
  geçer ama FK `23503` verir → **kayıt kaybı**. Evaluate/baseline.go bugün bunu ASLA üretmez (gerçek version
  id yükler); bu tam da CHECK+FK'nin erken yakalamak için var olduğu wiring-bug sınıfı. **M5-05 yazma yolunda
  ve denetiminde:** baseline/tenant kararında gerçek `policy_version_id` yüklendiğini + policy_context'in ham
  GPS değil mesafe taşıdığını (§4.7) doğrula.
- **🔴 N5 (M4-03 denetiminden) — M5 için BLOKLAYAN §4.5 tenant izolasyonu:** `tap.Decide` sıfır tenant-farkındalığıyla
  çalışıyor — `Input`'ta bugün **TagTenantID/SessionTenantID YOK**, dolayısıyla `sys:tenant-mismatch` guardrail'i
  ölü. Kritik incelik: tag çözümü (`GetTagByUID`) **context-less**'tır (ADR 0002 md.7) → **RLS çapraz-tenant tag'i
  çözümde GİZLEMEZ.** Tenant B çalışanı fiziksel olarak tenant A'da (IP/GPS eşleşir) A plaketine dokunursa,
  `sys:tenant-mismatch` beslenmediği sürece **`ok` check-in yazılır (izolasyon deliği).** Tek savunma bu
  guardrail'in beslenmesidir. **M5, `Input`'u `TagTenantID`+`SessionTenantID` ile genişletip Decide'a vermeli**
  (tag çözümünden tenant + oturumdan tenant). M4-03 bunu doğru şekilde M5'e erteledi (Decide karar taklidi
  yapmıyor); ama M5 bunu sağlamazsa delik açık kalır — **belt-and-braces değil, tek gerçek engel.** Ayrıca (düşük):
  Decide her redirect'i `RedirectActivation`'a eşliyor → M5 tenant-mismatch redirect'ini aktivasyondan ayırabilir.

### M6-02 / M6-05 / M7-02 / M7-04 / M8'e devralınan (M6-01 B'nin ON İKİ LİMİTİ)

Hiçbiri kapatıldı diye yazılmadı; hepsi **ölçüldü ve sayıldı**. Sahibi belli olanlar işaretli.

1. **🔴 M6-05 / M7-04 / M7-02 — digest-tarafı zamanlama kolu, ve SEBEBİ SÜRESİ DOLACAK.** Geçerli bcrypt
   digest'i olmayan bir admin satırı **154–198 ns**'de cevap veriyor (bcrypt anahtar programını kurmadan
   hata döner), kukla kolu **297,9 ms** → **~1,5–1,9 milyon×**. Yani 53× kehanetinin şekli **digest
   tarafından ve ters yönde** yeniden açılır. **Bugün erişilemez** (`db/queries` ve üretim Go'sunda
   `INSERT INTO admin_users` yok, `password_hash` UPDATE'i yok — tek `UPDATE admin_users`
   `MarkAdminLoggedIn`/`last_login_at`; seed'in iki satırı da `$2a$12$`; `Hash` boşu ve >72'yi reddediyor)
   **ama şemada format CHECK'i YOK**, `''` şema-geçerli. **Kural: `admin_users.password_hash` yalnız
   `adminauth.Hash` çıktısıyla yazılır.** Yapısal çözüm bir sütun CHECK'i = **yeni migration**, alınmadı.
2. **✅ ÖDENDİ (M6-02, `6757537`) — borç M6-03'e EL DEĞİŞTİRDİ, iptal edilmedi.** Bu üç madde
   *"`adminSessionLimit` kopyalandı"* · *"pay 1,5×"* · *"`adminFloodLimit` 10 parça ≈ 2000 varsayımıyla"* ·
   *"`sessionGate`'in sınırladığı iş boş"* diyordu. **Dördü de ölçümle çürütüldü:** M6-02 **HTMX getirmedi**
   (kullanıcı kararı: M6-03) ve gerçek sunucuda **bir sekme görüntülemesi = TAM 1 ücretli istek**
   (`/static` kapının **dışında**; 305 ardışık `GET /admin` → **300×200, #301'de 429**). Ölçülen meşru yük
   **~260/pencere** (200 görüntüleme + ~60 giriş), pay **11,5× (3000/260)** — eski öncülün ima ettiği
   **1,46× (3000/2060)** değil. ⚠️ **`300/20 = 15×` AYRI bir tavandır** (oturum başına, yönetici-başına
   payda) **ve doğrudur** — ikisini karıştırma; bu iki *"15×"*ten yalnız biri hataydı. **Üç sabit de
   DEĞİŞMEDİ.** Türetme artık `adminratelimit.go`'da, tavanların **yanında** yaşıyor (yalnız plan kartında
   değil) ve **paydayı gösteriyor**. **M6-03 parçaları getirince çarpan geri gelir → YENİDEN SAY**; eşik
   yazılı: **≥15 istek/görüntüleme**.
3. **M6-02 — `adminFloodLimit = 3000`'in maliyeti (referans, ödendi).** 12. turda kapı `Protect`'in
   **önüne** kondu (F-A'yı kapatmak için), böylece anonim kalkan ile meşru iş **tek kovayı** paylaşır oldu;
   tavan 200 → 3000 çıkarıldı. Maliyet **doğru kolda** ölçüldü: canlı oturum **3,0–5,7 ms** (resolver
   okuması **+ `TouchAdminSession` UPDATE**), uydurma token 0,65–1,21 ms →
   `3000 × 3,0–5,7 ms = 9–17 sn/pencere/adres = bir çekirdeğin %1,5–2,9'u`.
4. **M6-03 — `sessionGate` (300/oturum) kimlik doğrulamadan SONRAKİ işi sınırlar; M6-02 onu BEŞ ROTAYLA
   DOLDURDU ama her biri tek istek.**
   Ve **koruduğu maliyetin yanlış tarafında**: gate `requireAdmin`'den sonra koştuğu için **429 alan
   istek resolver okumasını ve UPDATE'i zaten ödemiştir** (ölçüldü: reddedilen istekte bile `last_used_at`
   değişiyor). Taşımak mümkün değil — oturumu çözmeden oturuma göre anahtarlayamazsın; `tap.go`'da sorun
   yok çünkü `httpx.Identify` bilerek **yazmıyor**.
5. **Çıkış 30000 üçüncü-taraf isteğinde reddedilebilir — invaryant ZAYIFLADI.** 14. turda çıkış
   *"asla reddedilmez"*di; 16. turda kendi tavanı kondu (`adminLogoutLimit = 10 × adminFloodLimit`) çünkü
   ölçüldü ki *"asla reddetme"* **sınırsız** bir yükselteç demekti (10000 anonim çıkış → **10000 resolver
   okuması, 0 red**, tap yüzeyinin **paylaştığı havuzda**). Şimdi üçüncü tarafın kurbanın çıkışını
   engellemesi **30000 istek** gerektiriyor — panelin geri kalanını reddetmenin **10 katı** ve flood
   log'unda gürültülü. **Bedeli:** `30000 × 0,65–1,21 ms ≈ 19,5–36 sn/pencere/adres ≈ çekirdeğin %3,3–6,0'ı`
   — **ürünün en geniş tavanı, flood tavanından pahalı.**
6. **Bastırılan deneme TOPLAMI DB'den kurtarılamaz.** Hesap audit bütçesi bir **iz susturma** primitifi:
   60 başarısızlık → **11 satır** (10 `failed` + 1 `rate_limited`). `rate_limited` satırı `SuppressedFrom`
   taşıyor (bastırmanın **başladığı sıra**, ölçüldü: 11) → *sessizlik değil kesinti*; müfettiş saldırıyı
   görür, **sayısını göremez**. Gerçek sayım pencere **kapanışında** bir satır ister; `httpx.Limiter`
   tembel tahliye ediyor ve süre-sonu kancası yok → **altyapı**.
7. **`-race` zamanlama kapısı 2,5× altını GÖRMEZ.** Kapı ölçülmüş gürültüye göre genişletildi (kullanıcı
   kararı 2026-08-03) ve **bir cost-adımlık kuklayı (1,91–1,99×) bilerek feda ediyor** — o vaka
   `TestCost_MatchesTheDummyDigest`'in **tam sayı** karşılaştırmasına bırakıldı. Gerçekten korumasız olan:
   ne eksik kukla ne cost uyuşmazlığı olan, **2,5× altındaki dördüncü bir şekil**.
8. **Sunucu tarafı panel oturum süresi YOK.** `admin_sessions`'da `expires_at` yok; 12 saatlik `Max-Age`
   bir **tarayıcı ipucu**. Gerçek kontroller: açık çıkış · *"her yerden çıkış"* (**rotası mount edilmemiş**)
   · `admin_users.status='disabled'` (**M6-05**). Düzeltmesi **migration**.
9. **Bilinmeyen e-postalı denemeler AUDIT'LENEMEZ.** `audit_log.tenant_id` NOT NULL + FK → atfedilecek
   tenant yok. Sıfırın **mekanizması** `failLogin`'in 0 adayla döngüye hiç girmemesi; **kısıt** boşluğun
   neden kapanmadığının gerekçesi. Kart kriteri bu yüzden **⚠️ KISMEN** işaretli.
10. **`GO-2026-5932` kabul edildi.** `golang.org/x/crypto/openpgp` bakımsız, **`Fixed in: N/A`**;
    yalnız `bcrypt` import ediliyor (4 satır, dördü de bcrypt), **0 vuln kodu etkiliyor** → `make audit`
    yeşil. **Yükselterek kapatılamaz.**
11. **Tip invaryantı AD-TABANLI.** `TestPackageTypes_NoExportedCredentialField` paketteki dışa açık
    struct'ları gezip **sır-benzeri ADI** olan ham alan arıyor (sabit listeli önceki hâli yeni **tipe**
    karşı çaresizdi — negatif kontrolle kanıtlandı). **Nötr adlı bir alan (`Value string`) kaçar.**
12. **🔴 M8 — SUITE GENELİ BAĞLANTI TÜKENMESİ, ÖNCEDEN VAR, M6-01 SEBEP DEĞİL.** `max_connections=100`
    − 3 rezerve = **97 slot**; `internal/db/invites_test.go` ve `internal/sun/advance_test.go`'nun
    kırmızı-çizgi yarış testleri **54'er** bağlantı açıyor (50 goroutine + 4) = **108 > 97**, yani
    **tek başlarına** sınırı aşıyorlar. Belirti: `TestConsumeInvite_ConcurrentRaceExactlyOneWinner` →
    `FATAL: sorry, too many clients already`. ⚠️ **Goroutine sayısını düşürmek bir §4.4 testini
    ZAYIFLATIR** (aynı `(tag, ctr)` ile N goroutine → tam 1 kazanan) → düzeltilmedi. Çözüm ya
    `max_connections` ya testcontainers ile izole DB — ikisi de **altyapı**.

### M6 / M7'ye devralınan (M5-11 denetimlerinden)

- **🔴 M6-04 — `audit_log` YOLU YOK, ve ADR 0008 bunu neredeyse "var" diye kaydediyordu.** Ölçüldü:
  `channel='manual'` satırı **408**, bunları hedefleyen `audit_log` satırı **0**, manuel kayıtla ilgili
  `action` **0**, manuel giriş için HTTP rotası **YOK**. Var olan: `checkin.Service.Record` domain yolu
  (`ErrEnteredByRequired` ile `entered_by` zorunlu). §4.3 *"düzeltme = yeni kayıt + `audit_log`"* diyor —
  **`audit_log` yazımı ve müdür yüzeyi M6-04'ün işi** ve bugün yok. ⚠️ `redline-check.sh` bunun
  **yokluğunu yakalayamaz** (audit_log'a bakmıyor), yani tek koruma bu satırdır.
- **🔴 M6-11 — maskelenmiş açık girişler ANOMALİ LİSTESİNDEN KENDİLİĞİNDEN DÜŞÜYOR.** `NOT EXISTS`
  `o.occurred_at > t.occurred_at` olduğu için **tek bir `out` kendinden eski TÜM açık girişleri kapatıyor**
  → maskelenmiş bir giriş, kişinin bir sonraki çıkışıyla "kapanmış" görünüyor ve o aralık **saat toplamına
  yanlış** giriyor. Ölçüldü: dev DB'de **47 hâlâ görünür, 43 zaten görünmez**. Geriye dönük tespit
  `p.practice AND p.occurred_at > t.occurred_at` sorgusunu gerektirir. **§4.6 ihlali DEĞİL** — hiçbir satır
  kaybolmuyor, kaybolan **sinyal**.
- **🟡 M5-05 K1 torbası — GERİYE TARİHLİ `out` HİÇBİR ZAMAN KAPATMIYOR.** Ölçüldü (practice'ten
  **bağımsız**, M5-11'in eseri **değil**): `in @13:16` altına `out @12:16` ve `out @11:16` yazılabiliyor,
  ikisi de **dangling** kalıyor, `in` **sonsuza dek açık** (dev DB'de **84** dangling `out`). Hafifletici:
  her geriye tarihli satır `base:queued-window`'u (120 sn, tenant ayarlı) aşıyor ve **flag** alıyor.
- **🟡 M7/M8 — `GetLastOpenTransaction` KİŞİ BAŞINA O(ÖMÜR BOYU SATIR) tarıyor** ve `practice` indekste
  **yok** (`Filter`, `Index Cond` değil). **DoS değil** (ölçüldü, üç ölçekte: sıradan şekilde yüklem
  **bedava** — 11 300 → 11 300 buffer; practice-en-üstte şekli bile sıradan şeklin **altında** kalıyor;
  `Timeout(30s)`'e ulaşmak kişi başına **~3 milyon satır** = iki aylık kesintisiz flood, ve zarar
  **saldırganın kendisiyle** sınırlı — kişi-bazlı advisory kilit). Ama §4.3 satırları kalıcı kıldığı için
  taranan küme **yalnız büyür**. Kısmi indeks bir **migration**'dır.
- **⚪ `make audit` üretilen kodu TARAMIYOR** ve bu **bilinçli**: `redline-check.sh` `GEN_EXCLUDE` ile
  `internal/store/*.go`'yu dışlıyor; **kaynak** (`db/queries/`, `SRC` içinde) taranıyor. Savunulabilir —
  ama R kuralları **kırmızı çizgi desenleridir**, yani *"bu rapor sorgusu `NOT practice` taşımalı"* gibi
  **anlamsal** bir kural **hiçbir katmanda** kontrol edilmiyor.

### M5-11 / M6 / M7 / M8'e devralınan (M5-10 denetimlerinden)

- **🔴 M6/M7 — GUARDRAIL SIRASI HÂLÂ ELLE BAKIMLI, ve ağın sınırı yazılı.** ADR 0007 sırayı
  düzeltti ve **yapısal** bir invaryant getirdi (`TestGuardrails_NothingUnnamedPreemptsAnAlert`:
  *uyarı taşıyan bir guardrail'in önündeki her şey adlandırılmış istisna olmalı*), iki kalıcı
  negatif kontrolle (`…RejectsTheRegressionOrder`, `…RejectsAnUnnamedEleventhGuardrail`).
  **Kalan boşluk yazılı:** istisna listesini (`namedAlertPreemptors`) küçültmek hâlâ mümkün —
  ama artık **görünür ve tartışılır** bir düzenleme. Yeni guardrail ekleyen herkes bu invaryantı
  okumalı; *"kırmızı testin listesini güncelle"* **yanlış onarımdır**.
- **🔴 M6/M7 — `sys:no-session` istisnasının gerekçesi BAŞKA PAKETTE yaşıyor.** `sys:no-session`
  (4) uyarı taşıyan `sys:employee-deactivated`'in (5) önünde ve bastırma **boş** — çünkü
  `internal/domain/tap/decide.go` `employee:status` anahtarını **ve** `SessionTenantID`'yi aynı
  `Employee != nil` dalında koyuyor, yani ikisi karşılıklı dışlayıcı. ⚠️ **`policy.Evaluate`
  DIŞA AÇIK bir API:** `employee:status`'ü `SessionTenantID`'siz set eden **ikinci bir çağıran**
  bunu boş bir ön-almadan **gerçek** bir ön-almaya çevirir. `internal/policy` bunu **zorlayamıyor**;
  sınır `guardrails.go`'da yazılı, ama pinlenmiş değil.
- **🔴 M6/M7 — `>900 sn` bandında NE KAYIT NE UYARI var** (ADR 0007 garanti-dışı #5). Ölçüldü:
  `GET /t` (deaktif) → **400**, `transactions +0`, **sıfır audit aksiyonu** → tenant tarafında
  **hiç iz yok**. Regresyon **değil** (M5-10 öncesi bant aynıydı) ve düzeltme kesin olarak
  iyileştirdi (uyarıyı düşürmek için gereken bekleme **3 dk → 15 dk**), **ama asimetri gerçek:**
  kapatılan yol satırı **yazıyordu**, hayatta kalan yol **hiçbir şey** yazmıyor — yani *"deaktif
  çalışanın denemesi kaydedilir"* yükümlülüğü 15 dakika beklemekle atlatılabiliyor. Sahibi:
  **M6/M7 uyarı teslimatı** (bugün `tap.security_alert`'in üretimde **hiçbir okuyucusu yok** —
  ADR garanti-dışı #3: satır garanti, **teslimat değil**).
- **🟡 M6/M7 — `retired` plakette uyarı hâlâ düşüyor** (garanti-dışı #1). `sys:tag-not-active` (2)
  `lost`'ta uyarıyı **daha acil** olanla takas ediyor (`lost-tag-tapped`) ama `retired`'da hiç
  üretmiyor. Güvenlik denetçisi §5 satır 4 ihlali **saymadı** (etiket durumu sunucu gerçeğidir,
  istemci seçemez; §5 satır 1 zaten satır 4'ün önünde) — **kabul edildi ve yazıldı**.
- **🟡 M8 — `floatEnvRange` `NaN`'ı SESSİZCE geçiriyor** (`config.go`). Ölçüldü: `NaN < 60 == false
  && NaN > 900 == false` → aralık kontrolü geçiliyor, `time.Duration(NaN×1s)` **int64 minimuma**
  dönüyor. **Freshness için M5-10'un eklediği kontrol yakalıyor** (başlangıçta patlıyor), ama
  **aynı helper'ın diğer iki çağıranında alt katman kontrolü YOK**: `TAPPA_GPS_RADIUS_M=NaN` →
  `config.Load` **err=nil** ve `checkin.New` **err=nil**; `TAPPA_DEBOUNCE_SECONDS=NaN` → sessizce
  60 sn'ye düşüyor. Etki **düşük** (NaN yarıçapta her mesafe karşılaştırması false → GPS-only
  tap'ler `flag`, yani **daraltıcı** yön; §4.6 korunuyor). Tek satırlık `math.IsNaN(v)` reddi üç
  çağıranı birden kapatır — kapsam dışı bırakıldı, `config.go`'da **SINIR** olarak yazılı.
- **M6-11 — *"POST'suz `GET /t` sayısı"* sinyalinin KAYNAĞI YOK.** İmzalı bağlam **stateless**;
  M5-10'un tablosu yapılmadığı için sunucu bir GET'in POST'a dönüşmediğini **bilemiyor**. O kart
  ya kendi kaynağını üretecek ya sinyali düşürecek. Asıl sinyal (`ctr` boşlukları) **çalışıyor**.
- **M5-11 — `sys:tap-freshness` artık CANLI**, yani M5-11'in gün testinde workaround'u kaldırırken
  sayfa yaşına da dikkat: `seedFreshness = 60 sn` (en sıkı yasal pencere) ve ölçüldü ki günün
  sonucu pencere değerine **duyarsız** (900'de de yeşil) — `tapNFC`/`tapQR` GET+POST'u tek çağrıda
  yapıyor, `dayWait` uykuları fazlar **arasında**.

### M5-10 / M5-11 / M6 / M8'e devralınan (M5-09 denetimlerinden)

- **🔴 M8 / M0-06 — CI OLDUĞU GİBİ KIRMIZI VERİR VE BUNU BUGÜNE KADAR KİMSE GÖRMEDİ.**
  Repoda **uzak yok** (`git remote -v` boş — kullanıcı kararı: push/PR yok), yani `ci.yml`
  **hiç çalışmadı**; "CI yeşil" bu projede **teorik** bir cümle. Ve olduğu gibi koşarsa
  kırmızı verir: workflow `DATABASE_URL`/`DATABASE_MIGRATE_URL` veriyor ve `make up`
  koşuyor (yalnız Postgres'i **başlatır**), ama **`make migrate` koşmuyor**, `make seed`
  koşmuyor ve **`TAPPA_TAG_KEK` vermiyor** → `make check` → `make test` DB testlerini
  **migrate edilmemiş** şemaya karşı sürer. Ölçüldü (boş DB'ye karşı `make test`):
  **17 pakette 140 üst düzey FAIL**, bunların **136'sı M5-09'dan eski** — yani bu bir
  gerileme değil, **M1-09'dan beri taşınan** bir durum. Düzeltme tek satır değil:
  `make migrate` + `TAPPA_TAG_KEK` + `make seed` (dördü seed'e bağlı) gerekiyor, ve
  **uzak olmadan doğrulanamaz**. Bu kapanana kadar `make check`'in tek gerçek koşum yeri
  **geliştiricinin makinesi**dir.
- **🔴 M6 — koşum damgası kullanıcıya görünen yüzeye SIZIYOR.** Gün simülasyonu çalışanları
  `Maria Borg [sim 08-02T00:40:24 f4ef]` diye üretiyor; `full_name` → `EmployeeName`
  (`internal/domain/tenant/directory.go:109`) → aktivasyon/tap ekranındaki *"Hello …"*.
  Yani bugünkü dev-DB **ekran görüntüsüne hazır değil** ve M6 dashboard'u da `full_name`
  okuyacak. **Değerlendirilmemiş üçüncü yol** (kartta yazılı): günü `make test`'ten ayırıp
  **yalnız `make simulate-day` içinde seed'li kadroyu** sürmek — hem yeniden koşulabilirliği
  hem demo amacını karşılardı. Damgasız sabit kadro bugün **DB ömrü başına bir kez**
  ağırlanabiliyor (ölçüldü: 2. koşuda tur atlanıyor, `practice=false`, önceki koşumdan
  **açık giriş devralınıyor**) — §4.3 sonucudur, sondanın sınırı değil.
- **🔴 M5-11 — sevk edilmiş §5 ihlali** (practice satırı açık girişi maskeliyor). Kartı
  açıldı, kullanıcı kararı 2026-08-02. Düzeltilince M5-09'un `LIMITS L3`'ü ve kart md. 6'sı
  **kapatılmalı** — yoksa repoda kapanmış bir kusuru açık gösteren iki cümle kalır.
- **M6-07 — `MinutesLate` hem YANLIŞ HESAPLANIYOR hem HİÇBİR YERE YAZILMIYOR.**
  `decide.go` `lateness()` `in.Now`'u (sunucu saati) kullanıyor, kaydın `OccurredAt`'ini
  **değil** → 10:00 vardiyasına 10:17 beyan eden geriye tarihli giriş **`-520`** döndürdü.
  Canlı tap'te ikisi çakıştığı için görünmüyor, ama `occurred_at` sevk edilmiş bir alan ve
  tavanı **72 saat**. Ayrıca `minutes_late` **sütunu yok**, `policy_context`'e yazılmıyor
  (`time:minutesLate` `document.go`'da tanımlı ama `Decide` set etmiyor) → o anahtara
  yazılmış bir tenant politikası **sessizce hiç eşleşmez** (M3-04 invariant'ı). Bu yüzden
  skill'in *"geç kalma"* senaryosu **HTTP'de üretilemiyor** ve M5-09 onu üretmedi.
- **🟡 M6/M7 — `tags.uid` CHECK'i İKİ YAZIMA izin veriyor, zarfın AAD'si ise HAM 7 BAYT**
  (güvenlik denetçisi, ORTA). `hex.DecodeString` büyük/küçük harf duyarsız + `CHECK (uid ~
  '^[0-9A-Fa-f]{14}$')` → `04AC7E55000601` ile `04ac7e55000601` **iki ayrı PK satırı, aynı
  AAD**; ölçüldü: bir plaketin zarfı diğerine **açılıyor**, `last_ctr`'ler **bağımsız**, ve
  çapraz-tenant ikinci satır **INSERT edilebiliyor**. **Bugün sömürülemiyor** — `sun.Parse`
  UID'yi büyük harfe **kanonikleştiriyor** (3/3 yazım aynı satıra düştü) ve `tags`'a INSERT
  eden **hiçbir üretim yolu yok**. Latent: **M8-05** plaket kayıt akışı operatörün yazdığı
  uid'yi olduğu gibi eklerse sonuç **ölü plaket** olur (kaydedilir, hiç tap almaz, hata
  vermez). Çözüm **ikinci temsili silen** tek şey: yeni migration, `CHECK (uid ~
  '^[0-9A-F]{14}$')` (00004 uygulanmış, §6 — yenisi yazılır). Mevcut 12 satır zaten büyük
  harf, veri taşıması yok. **Bu, "kontrol ile tüketici aynı temsili görmeli" sınıfının bu
  oturumdaki DÖRDÜNCÜ vakası.**
- **🟡 M6/M7 — `tappa_app` `tags`'ın DOKUZ sütununda da UPDATE'e sahip** (güvenlik denetçisi,
  ORTA). Ölçüldü: `aes_key_ref`'i bozabiliyor (DoS), `uid`'i yeniden adlandırabiliyor (DoS)
  ve **`last_ctr`'ı 0'a GERİ SARABİLİYOR** (§4.4 replay penceresi). Bugün erişilebilir değil
  (hiçbir üretim sorgusu bu sütunları yazmıyor — `tags` üzerinde tek sqlc sorgusu
  `AdvanceTagCounter`; repoda **dinamik SQL yok**). ⚠️ **Sütun-düzeyi grant TEK BAŞINA
  yetmez:** `last_ctr` listede kalmak **zorunda** (§4.4 advance onu yazar) ve en ağır yetenek
  tam olarak onu geri sarmak. Gerçek çözüm: `REVOKE UPDATE` + `GRANT UPDATE (last_ctr,
  status, retired_at, replaced_by)` **artı** monotonluk trigger'ı (`NEW.last_ctr >
  OLD.last_ctr`).
- **🟡 M8 — `redline-check.sh` kapsamı bir DENETİM İDDİASININ PARÇASIDIR.** `SRC` `test/`'i
  içermiyordu → M5-09'un dört yeni dosyasının **ikisi** hiç taranmıyordu ve *"make audit
  temiz"* göründüğünden az şey kanıtlıyordu. `test` eklendi (ölçüldü: **sıfır** yanlış
  pozitif). Kalan iki sınır: R7 desenleri **tek satırlık**, `fmt.Fprintf(&b,` + duyarlı
  dize sonraki satırda olunca ağdan geçiyor (ölçüldü, `-U` ile yakalanıyor); ve `SRC`'ye
  eklenmeyen her yeni dizin sessizce denetim dışıdır.
- **🟡 M6/M8 — rota başına flood tavanı hâlâ PİNLENMEMİŞ** (M5-07'den devam). M5-09 bunu
  değiştirmedi.
- **⚪ `make help` her hedefi `Makefile` diye yazıyor** — `-include .env` `.env`'i
  `MAKEFILE_LIST`'e sokuyor, grep dosya adı önekliyor. Tek karakterlik düzeltme (`grep -h`),
  M5-09'un işi değildi, commit'e karıştırılmadı.

### M5-09 / M5-10 / M6 / M7 / M8 / M9'a devralınan (M5-08 denetimlerinden)

- **🔴 M8 — DAĞITIM: hiçbir katmanda timeout YOK.** Küme, veritabanı **ve** rol seviyesinde
  `statement_timeout` · `lock_timeout` · `idle_in_transaction_session_timeout` **üçü de 0** (ölçüldü).
  Tek tavan HTTP'deki `middleware.Timeout(30s)`. Advisory kilit geldiğine göre bu artık **kayıp
  penceresinin** de tavanı: `advance` ile `INSERT` arasında 30 sn'ye kadar beklenebilir.
- **🔴 M6-01 / M6-04 — `channel` trust'ın YANINDA gösterilmeli.** `trustScore`'da kanal terimi yok
  (§5 normatif): IP+GPS'li QR **100**, IP+GPS'li NFC **100**. Ayrımı yalnız `transactions.channel`
  taşıyor ve **hiçbir kullanıcı yüzeyinde görünmüyor**. Gösterilmezse müdür `Trust 100`'e bakıp NFC
  sanar. (Kartın *"QR asla NFC trust'ına çıkmaz"* cümlesi bu ölçümle **kanıt tavanı** okumasına
  indirildi: QR aynı kanıtla asla insansız `ok` olamıyor — sayı çakışsa bile.)
- **🔴 M7-03 — politika materyalizasyonu eşzamanlılıkta 23505 alıyor.** `EnsureBaselinePolicy`
  `ON CONFLICT (id)` ile hakemlik yapıyor ama `policies` ayrıca `policies_id_tenant_key (id, tenant_id)`
  taşıyor (00007, FK hedefi) → spekülatif insert **hakem olmayan** indekste yakalanıp düşüyor.
  Gerçek kod yolunda ölçüldü: 40 yarışçı → 3–4/200 bozuk politika seti, log `ensure baseline policy …
  policies_id_tenant_key`. **Fail-safe** (satır yazılır + `flag` + ERROR log, §4.6'nın tarif ettiği
  davranış) ama bakir tenant'ın ilk patlamasında `ok` yerine `flag`. ⚠️ **`ON CONFLICT (id, tenant_id)`
  ÇÖZMÜYOR, kötüleştiriyor** (çarpışmayı `policies_pkey`'e taşıyor, ölçüldü); **PK hakemliği de
  çalışmıyor** (`policy_versions` PK hakemliğinde 9/100). İki unique indeks varken **bilinen çalışan
  bir hakem seçimi yok** — gerçek çözüm 23505 retry / iki indeksi de adlandıran upsert / sign-up'ta
  provisioning.
- **🔴 M9-01 — çevrimdışı kuyruk BU KURALLA KIRILIYOR.** Aynı senkronda saniyeler arayla gönderilen
  iki kuyruklanmış tap'in ikincisi `ignored` olur (ölçüldü: 7 sa arayla `occurred_at`, saniyeler arayla
  POST → `sys:person-debounce`). ADR 0006 bunu **ifşa ediyor, gerekçelendirmiyor** — M9-01 kuyruğu
  tasarlarken kanal/işaretle ayırmak zorunda.
- ~~**M5-10 — tazelik penceresi tavanı düşürüyor…** 3 dk'lık pencere ikinci pencereyi siler, tavanı
  kabaca **yarılar**.~~ **🔴 BU VAAT YANLIŞTI — düzeltildi 2026-08-02 (M5-10 denetimi).** `sys:tap-freshness`
  guardrail'i **NFC-only**'dir ve bu **bilinçlidir** (M3-05; §5: *"QR fotoğraflanır ve süresiz geçerlidir"* —
  QR'da sayfa yaşı diye bir kanıt yok), `TestGuardrails_SunInvalidExemptsQR` o günden beri pinliyor.
  Dolayısıyla **M5-10 QR tavanını DEĞİŞTİRMEDİ**: tek taranmış QR bağlamı bugün de **15 dk TTL** boyunca
  yeniden POST edilebiliyor. Frenler değişmedi — `base:qr-requires-ip` · 60 sn kişi-debounce (fazlaları
  **kayıtlı `ignored`** yapar) · `tapSessionLimit` 300/10dk + `ByAddress` 3000/10dk. Yapısal tavan
  (~600, TTL'e iki pencere) **aynen duruyor**. Sahibi: QR tavanını gerçekten daraltmak isteyen bir görev
  (M6-11 sinyal tarafı / M8 paylaşılan store), M5-10 değil.
- **Kilidin ölçülmüş bedeli (M8 kapasite planı):** bekleyen istek **havuz bağlantısı tutuyor**; tek
  anahtara inen flood'da ilgisiz kişinin gecikmesi **6–9×** (16 bağlantının 15'i `wait_event='advisory'`).
  Tavanlar `ByAddress` 3000/10dk ve 30 sn. **Ölçüm yöntemi de yazılı** (flood **ayrı oturumlardan**,
  kurban **tek atış**) — aksi hâlde `BySession` 300/10dk artefaktı *"fark yok"* dedirtiyor.
- **`SecondsSinceLastRecordedTap` taramayı daraltmıyor.** Pencere yüklemi **sıralamayı** sınırlıyor;
  indeks `(tenant_id, employee_id, occurred_at)`, `created_at` içinde yok → Bitmap Heap Scan kişinin
  tüm geçmişini geziyor (buffer sayısı yüklemli/yüklemsiz **birebir aynı**). §4.3 satırları kalıcı
  kıldığı için taranan küme **yalnız büyüyor**, en hızlı **flood edilen kişide**. `created_at` indeksi
  = migration, alınmadı.

### M5-09 / M6 / M8'e devralınan (M5-07 denetimlerinden)

- **Rota başına flood tavanı PİNLENMEMİŞ.** Beş `flooded` çağrısından yalnız `activate_submit` bir
  testle tutuluyor (`TestBudgets_FloodCeilingStillRefuses`); `activate_tour` **ve** `activate_done`
  gate'i silinince suite **yeşil** kalıyor (ölçüldü). M5-07 mevcut boşluğa **beşinci üye ekledi,
  boşluğu açmadı**. Rota başına *"floodLimit+1. istek 429"* tablo testi **kendi görevini hak ediyor**.
  ⚠️ **2026-08-03: bu satır AKTİVASYON ailesi için hâlâ doğru** (`activate.go`'da hâlâ tam 5 çağrı, hâlâ
  yalnız `activate_submit` tutuluyor) **ama artık eksik konuşuyor:** M6-01 B panelin **beş** flood çağrısını
  bir tablo testiyle **pinledi** (`TestAdminAuth_FloodCeilingRefusesEveryUnauthenticatedRoute`, kapsanan
  rota **5**), yani boşluk **yarıya indi**. Kalan borç: `activate_tour` · `activate_done` — ve o üç çalışan
  rotası **altyapı** istiyor (Tap/Activation koşum takımı: DB, davet yöneticisi, oturum yöneticisi, SUN
  doğrulayıcı), M6-01'in kapsamı değildi.
- **Dokunma hedefi ölçümü markup okur.** `TestTour_HasExactlyTheseTouchTargets` HTML'e bakıyor;
  **salt CSS ile** basılabilir hale getirilmiş bir alanı (dev `::after`) hiçbir test göremez. Kapalı
  küme içinde basılabilir olan yalnız `<a href>` (`button`/`form`/`input`/`area`/`label`/`details`
  kümenin dışında), o yüzden bugün dar; ama piksel ölçen test yok.
- **Turun *"never counts toward your hours"* cümlesi GELECEK ZAMANLI.** Repoda saat toplama **yok**
  (0 eşleşme); sahibi **M6-07** ve o kart `practice = true` kayıtların saate dahil olmadığını zaten
  yazıyor. Bugün sınanabilen kısım yalnız **yön zinciri**.
- **Tur `Activation.render`'dan geçiyor → CSP yok** (aşağı md.3, dokuz ekran). Güvenlik denetçisi §4
  açısından kabul edilebilir buldu: turda form yok, script yok, `<a>` dışında basılabilir eleman yok.

### M5-08 / M6'ya devralınan (M5-06 denetimlerinden)

1. **`checkin.go:205` outcome switch'inin `default:` dalı Result ("recorded") ekranını render ediyor.**
   Bugün yalnız `OutcomeRecorded` oraya düşüyor, ama ileride *"yazılmadı"* anlamına gelen bir outcome
   eklenirse **kaydın var olduğunu söyleyen ekranı miras alır** (§4.6). Doğrusu `case OutcomeRecorded:`
   + gürültülü default. **M5-05'ten geliyor**, M5-06 dokunmadı (kapsam).
2. **`sys:tenant-mismatch` dalı `audit_log` satırı YAZMIYOR** (`checkin.go:560-563`, yalnız `slog.Warn`;
   `ActionTapUnknownTag` var, bunun karşılığı yok). Güvenlik denetçisi: bir tenant'ın oturumunun başka
   tenant'ın plaketine dokunması **anlamlı bir olay** ve DB'de izi kalmıyor → §4.5 kanıtı **log
   rotasyonuna bağlı**. §4.6 ihlali değil (o yolda yazılacak meşru mesai kaydı yok). **M6** (dashboard
   anomali sinyalleri) ya da daha erken.
3. **DOKUZ aktivasyon ekranında CSP yok** (M5-07 denetiminde yeniden sayıldı: `Activation.render`
   `Content-Security-Policy`'yi **0 kez** set ediyor → `Activate` · `Confirm` · `Done` · **`Tour`** +
   beş `problem*` sabiti). ⚠️ **2026-08-03 güncellemesi: artık İKİ** `Header().Set("Content-Security-Policy")` var
   (`tap.go:602` + `adminlogin.go:978` — M6-01 B panelin **gövdeli her yanıtına** CSP koyuyor); aşağıdaki
   "tek" ifadesi M5-07 dönemine aitti. Aktivasyon ailesinin **dokuzu** hâlâ korumasız. Eski hâliyle: `internal/handler`'da tek `Header().Set("Content-Security-Policy")` var
   (`tap.go:602`) → **tap ailesinin altısı** korunuyor, aktivasyon ailesinin **dokuzu** korunmuyor;
   ortak `pages.Problem` şablonu ikisine birden hizmet ettiği için asimetri aynı şablonun içinden
   geçiyor. Aktivasyon `Message` dizeleri de o pakette pinli değil. Üç denetçi "düşük riskli, asimetriyi
   kaldırır" dedi ama **M5-02'nin akışı** olduğu için dokunulmadı. *(M5-06'da bu satır "beş" diyordu;
   M5-07 `Tour`'u ekleyip sayıyı 8'den 9'a çıkardı ve doğru sayım o denetimde yapıldı.)*
4. **CI `make css` koşmuyor** (`ci.yml`: yalnız `make tools`/`up`/`check`/`audit`; `app.css` gitignore'da)
   → derlenen CSS'i okuyan **iki test de** (`TestCompiledCSS_GeneratesNoText` ve `TestCompiledCSS_StampWordIsInk`)
   **CI'da daima SKIP**, ve bir skip **pass değildir**. Hem CSS ile üretilen metin kanalı hem damga
   renginin ink kaldığı yalnız geliştiricinin makinesinde, elle `make css` sonrası korunuyor.
   **M8** (ya da tek satırlık CI adımı).
5. ~~**Damga kontrastı — kullanıcı kararı bekliyor.**~~ **KAPANDI — kullanıcı kararı 2026-08-01:**
   *damga METNİ `ink`, durum RENGİ çerçevede.* Eşleme korundu, palete yeni token girmedi. Ölçüldü
   (`paper #FFFDF4` üstünde, damganın `.docket` içinde olduğu render edilen HTML'den doğrulandı):
   **ÖNCE** approved 7.73 / flagged **2.62** / rejected 5.30 / ignored **1.52** / training 16.17, ve
   `.stamp`'in `opacity:.8`'iyle efektif 4.70 / **2.14** / **3.77** / **1.39** / 8.54 → **beşten üçü AA'nın
   altındaydı**. **SONRA** 13.85 / 14.81 / 13.99 / 15.55 / 13.27 — hepsi geçiyor. `opacity-80` **kaldırıldı**
   (AA için değil: çerçeve artık tek renk taşıyıcı ve grup opaklığı onu da soluklaştırıyordu).
   **Kalan sınır (yazılı, kabul edildi):** çerçevenin kendisi WCAG 1.4.11'in **metin-dışı 3:1** eşiğini
   `saffron` (2.62) ve `line` (1.52) için geçmiyor — durumu **kelime** taşıdığı için kabul edildi.
6. **Ekran-başına elle kapsam.** M5-06'nın üç beyaz listesi (metin · eleman · referans) **ekran başına**
   ve **elle**: bu pakete sonradan eklenen bir şablon, biri onu **iki listeye de** yazana kadar hiçbiriyle
   kapsanmıyor. Hiçbir şey bunu zorlamıyor — yazılı sınır.

### M5-08 / M5-10 / M6'ya devralınan (M5-05 denetimlerinden)

1. **🔴 `tap:trust` · `tap:direction` · `tap:practice` · `time:minutesLate` policy context'inde YOK**
   (Evaluate **sonrası** hesaplanıyor — M4'ten miras, kötüleştirilmedi). Bunlara yazılmış bir **tenant
   politikası sessizce hiç eşleşmez**. **M6-09** (policy yönetim ekranı) politika yazma yüzeyini
   açmadan **önce** ele alınmalı — yoksa müşteri çalışmayan kural yazar ve bunu fark etmez.
2. **Sayaç kayıttan ÖNCE harcanıyor.** `advance` ile `insert` arasındaki **her** hata (altyapı hatası
   **ve** istemci bağlantı kesme) o basışın kanıtını götürür: ölçüldü, `transactions` **+0**,
   `last_ctr` **700→701**. Kalıcı kayıp değil (yeni dokunuş yeni `ctr` üretir), **sessiz de değil**
   (ERROR log + "Nothing was recorded … Tap the plaque again"). Bugün dar (`tappa_app` `tags`/
   `employees` silemez). Yazma'yı istekten koparmak **daha kötü** olurdu (terk edilmiş istek mesai
   kaydeder).
3. **`transactions` artık bir YAZMA BÜTÇESİ.** 40 POST → **40 silinemez satır**; bütçeler 300/10dk
   (oturum) + 3000/10dk (adres) ⇒ ~43k/oturum/gün, ~432k/mekân-adresi/gün. Atfedilebilir olduğu için
   sahtecilik değil **gürültü/depolama** — ama M5-02'nin *"koruma maliyeti saldırı olmasın"* argümanı
   **yalnız `audit_log` için** kurulmuştu. **M8** (paylaşılan store + mekân-başına anahtarlama).
4. **`acc` (GPS doğruluğu) kimse tarafından okunmuyor** — sütun yok, kural yok; **5 km'lik bir fix
   sıkı olanla aynı sayılıyor**. Trust puanı sahte-GPS'e karşı bunu kullanabilirdi (ADR 0005 A3).
5. **Debounce temeli herhangi bir verdict** — 10 sn önceki bir `reject` gerçek bir tap'i yutabilir.
6. **QR bağlamı TTL boyunca tekrar POST edilebilir** (ilerletilecek sayaç yok) → **M5-08**'in işi.
7. **Oturumsuz POST tag'e atfedilemiyor** (bağlam oturum id'si üzerinden MAC'li) → 00005'in çalışansız
   reject satırı bugün **erişilemez**.
8. ~~**Damga sınıfları:** literaller artık `.templ`'de; `app.css` gitignore'da…~~ **KAPANDI (M5-06).**
   Taze build ile doğrulandı: beş damga sınıfı derlenen CSS'te (`approved` 1 · `flagged` 2 · `rejected` 2 ·
   `ignored` 2 · `training` 2, `grep -o | wc -l`), renkler palete birebir, negatif kontroller 0. Ayrıca
   **kural genelleşti:** CSS sınıf adı üreten Go kodu **sıfır** — üretim kodunda sınıf adı yalnız taranan
   `.templ` dosyalarında literal olarak yaşıyor.

### M5-05'e devralınan (M5-04 denetimlerinden)

1. **🔴 `sun.Verify` POST'ta ÇAĞRILAMAZ.** İmzalı bağlam CMAC **taşımıyor** (kart tuzağı gereği) →
   CMAC'siz `Params` ile `Verify` çağrılırsa `verifyMAC` **false** döner, `SUNValid=false`, sayaç
   **hiç ilerlemez** (fail-closed ama her NFC tap flag'e düşer). Sözleşme:
   **`sunValid == ctx.CMACVerified && AdvanceCounter başarılı`**. `AdvanceCounter` zaten exported ve
   `WithTenant` ister. Kart bu satırda düzeltildi.
2. **Tag'i POST'ta YENİDEN ÇÖZÜMLE** — durum GET ile POST arasında değişebilir (§5 satır 1:
   `lost`/`retired` → `reject`). `Preview` artık `TagStatus` taşıyor ama o **GET anının** durumu.
3. **🔴 QR bağlamı TTL boyunca tekrar POST edilebilir.** NFC'de atomik ilerletme durdurur;
   **QR'da ilerletilecek sayaç yok** → tek savunma **60 sn debounce** (person-scoped, guardrail'de).
   M5-08 QR kanalını ele alırken bunu bilmeli.
4. **Yabancı-tenant `ctx`'i** base64 çözülünce o tenant'ın **UUID'lerini** açıyor (ad değil, opak
   kimlik; plakete fiziksel dokunmuş birine). Denetçi §4.5 ihlali saymadı — sertleştirmek isteyen
   şifreleme ekleyebilir.
5. **Seed `aes_key_ref` KEK-sarmalı DEĞİL** → seed'li plaketler NFC yolunda **500** veriyor
   (`unwrap: wrapped ref must be 44 bytes, got 42`). M5-05'i **bloklamıyor** (birim/DB testleri kendi
   plaketlerini üretiyor) ama **M5-09'u ("bir günü simüle et") HTTP üzerinden NFC ile BLOKLAR**.
   Çözüm: seed `sun.Wrap(kek, uid, key)` ile **yapısal olarak doğru** sahte anahtar üretsin (§4.7:
   gerçek anahtar repoda yer almaz). Backlog **T7** ile aynı kök.
6. **CSP yalnız tap yanıtlarında** — aktivasyon ekranları (M5-02) bilinçli olarak dokunulmadı
   (o akış dört denetim turu gördü, kendi görevini hak ediyor).

### M5-04 / M5-05'e devralınan (M5-03 denetimlerinden)

1. **🔴 `TapLimiter`'ı MONTE EDECEK OLAN M5-04/M5-05'tir** ve montaj **sırası bir sözleşmedir**:
   `ByAddress` (DB işinden **önce**) → `Identify` → `BySession`. Sıra bozulursa `BySession`
   `SessionUnresolved` görür ve **500** verir (bilerek gürültülü: ölçülmeyen istek sessizce geçmesin).
2. **🔴 429'un §4.6 kalıntısı — adıyla yazılı, çözülmedi.** Bir mekânın paylaşılan adres bütçesini
   düşmanca bir cihaz harcarsa o mekânın **meşru tap'leri 429 alır** ve istek **karar motoruna hiç
   ulaşmaz** → ne `transactions` satırı ne `flag`. §4.6 tam da bunu `flag` ile karşılamak için var.
   Sınırlandı, çözülmedi (M8: paylaşılan store + mekân-başına anahtarlama).
3. **`Identity` sıfır değerinde `Err == nil` VE `!Live()`** → `if id.Err != nil {500} else if !id.Live()
   {aktivasyon}` yazan bir handler tam **§5 satır 3**'e düşer. `identity.go` dikkatli yazılmış ("artık
   olamaz" demiyor), ama M5-04/M5-05 bu ayrımı **açıkça** ele almalı.
4. **§5 satır 3 yönlendirmesi M5-04'ün işi.** M5-02 hedefi (aktivasyon sayfası) kurdu ve `transactions`
   yazmıyor; oturumsuz tap'in oraya yönlendirilmesi bağlanmadı. ⚠️ M5-02'nin **koşullu** çerez-ekim
   savunmasıyla birleşiyor: `GET /t` oturumsuz tap'i `/activate`'e yönlendirdiğinde, ekili bir davet
   çerezi olan tarayıcı **yabancı tenant'ın formunu** görebilir (form artık hangi işletme/çalışan
   olduğunu butonun üstünde yazıyor — tek gerçek engel bu).
5. **Fontlar self-host DEĞİL** (`web/static/fonts/` yok, `@font-face`=0, sistem fontuna düşüyor,
   **dış istek yok**) → M5-04 kabul kriteri.
6. **429 gövdesi düz metin** — markalı sayfa `tappa-brand` ile M5-04'te.
7. **Limiter süreç-içi ve sabit-pencere**; iki instance sınırı ikiye katlar, pencere sınırında 2×limit
   mümkün. `limiterMaxKeys` 100k aşılınca map **toptan sıfırlanıyor** (fail-open, bilinçli ve yazılı).
   Hepsi M8 (paylaşılan store).
8. **`TAPPA_TRUSTED_PROXIES` kalan sınırı:** kapı yalnız **tek girdilik** `/0`'ı yakalar; `/1` ya da
   birleşimle tüm uzayı kaplayan liste (`0.0.0.0/1,128.0.0.0/1`) **yakalanmaz** — yazılı. Ayrıca
   güvenilen aralık **gerçek istemcileri içeriyorsa** onlar serbestçe uydurabilir (ölçüldü). M8 dağıtım
   kontrol listesi. Proxy'nin XFF'e **append** etmesi zorunlu; **replace** eden proxy buradan
   ayırt edilemez.

### M5-03'e devralınan (M5-02 B denetimlerinden)

1. **🔴 ADLANDIRILMIŞ YÜKÜMLÜLÜK — gerçek istemci IP.** `internal/handler` `clientIP` bilinçli olarak
   **`X-Forwarded-For` okumuyor** (M5-03'ün işi). Sonucu ölçüldü: ters proxy arkasında **her istek tek
   anahtarı paylaşır** → flood tavanı (600/10dk) **küresel**, yani dışarıdan tek bir çağıran tavanı
   harcayıp o pencerede **herkesi** bloklayabilir; `unknown` bütçesinin log bastırması da küresel.
   M5-02'de **ucuz** hâli kapatıldı (60 bilinmeyen kod artık geçerli bir aktivasyonu reddetmiyor — üç
   ayrı bütçe), ama kalanı bir sayı ayarı **değil**. M5-03 gerçek IP'yi çözmeli **ve `floodLimit`'i
   yeniden değerlendirmeli**. Kartın "gerçek IP yalnız `cfg.TrustedProxies` hop'larından" kriteri
   birebir bu iştir; chi `middleware.RealIP` başlığa **koşulsuz** güvendiği için kullanılmaz (R5).
2. **Oran sınırlayıcı süreç-içi** — iki instance sınırı ikiye katlar; sabit-pencere (fixed window)
   sınırında kısa sürede 2×limit mümkün. İkisi de `ratelimit.go`'da sınır olarak yazılı → paylaşılan
   store M8'de.
3. **Aktivasyon çerezi ekimi oturumsuz telefonda durdurulmuyor** (önlem 3 koşullu: yalnız *çakışan
   oturum + çapraz-site* dalında ateşler). Bugün ek yetenek vermiyor (saldırgan aynı linki doğrudan da
   gönderebilir — ADR 0005 Y-D kimlik avı). **Ama M5-04 devir riski:** `GET /t` oturumsuz tap'i
   `/activate`'e yönlendirecek, yani ekili çerezi render eden sayfaya. Form artık **hangi işletme +
   hangi çalışan** için aktive olunduğunu butonun hemen üstünde yazıyor; tek gerçek engel bu.
   `Sec-Fetch-Site` **yoksa same-site sayılıyor** (fail-open, yazılı).
4. **Çerez gölgeleme:** aynı isimli iki `tappa_activation` çerezinde `r.Cookie` **ilkini** alıyor
   (alt alan adı kontrolü gerektirir). `__Host-` öneki M8'e yazılı.
5. **`ConfirmView.Code` düz `string` kalmak ZORUNDA** — templ bir değeri render ederken string biçimini
   ister, redakte eden tip `invite.Code(redacted)` **post ederdi**. Beyan edilmiş tek istisna, tek
   görünüm, tek yol; koruma tip sistemi değil **inceleme**. Yeni bir görünüme kod alanı eklenirse bu
   muhakeme tekrarlanmalı.
6. **§5 satır 3 BAĞLANMADI** — M5-02 hedefi (aktivasyon sayfası) teslim etti ve **`transactions` satırı
   yazmıyor**; oturumsuz tap'in oraya yönlendirilmesi **M5-04**. Fontların self-host edilmesi de M5-04
   (bugün `web/static/fonts/` yok, `@font-face` = 0, sistem fontuna düşüyor, **dış istek yok**).
7. **Davet üreten HTTP uç noktası YOK** (bilinçli): admin auth **M6-01**. Kimliksiz bir uç nokta tam da
   Y-D riskini genişletirdi. Q02 çözülene kadar gönderim **arayüz ardında**; kod çalışanın kendi
   kanalına gidince Y-D daralır (ADR 0005).

### M5-02 / M5-03'e devralınan (M5-01 denetimlerinden, bloklamayan)

1. **🔑 Deaktif çalışanın çerezi CANLI oturum olarak çözülmeye devam eder** (3. tur denetçisi, en önemlisi).
   Bilinçli: `resolve_session_by_token_hash` `employees.status` döndürmez ve deaktivasyon oturumları
   **iptal etmemelidir** (aşağı md. 2). Sonuç: **tap yolu dışındaki** her kimlik doğrulayan yüzey
   (M5-03 middleware, ileride herhangi bir çalışan sayfası) `employees.status`'ü **kendisi** kontrol
   etmek zorunda. Tap yolunda otorite `sys:employee-deactivated` guardrail'idir; başka yerde otorite yok.
2. **Deaktivasyon (M6-05) `RevokeAllForEmployee`'yi ÇAĞIRMAZ.** Denetçi kanıtladı: `decide.go:96`
   `CtxEmployeeStatus`'ü doğrudan `Employee.Status`'ten kurar ve `sys:employee-deactivated` yalnız ona
   bakar → iptal reddetmeye hiçbir şey **katmaz**, yalnız §4.6 kayıp koşulunu üretir (guardrail sırası:
   `sys:no-session` **#6**, `sys:employee-deactivated` **#7** → iptali "oturum yok"a çeviren çağıran
   önce redirect alır, **kayıt yazılmaz**). Meşru çağıranlar: çalınan/kayıp telefon, M5-02 ikinci aktivasyon.
3. **`Verify` API tuzağı — `if err != nil { aktivasyon }` YANLIŞ.** `ErrNoSession` için doğru,
   **`ErrRevoked` için değil**: `Verify` iptalde **dolu `Resolved`** döndürür (çağıran §5 satır 4'ü
   uygulayıp kaydı yazabilsin). Sözleşme `manager.go`'da 3 adımda yazılı. Tip zorlaması **bilinçli
   yapılmadı**: `(Resolved, Outcome, error)` şekli çağıran Outcome'u kaçırırsa iptal edilmiş çerezi
   CANLI sayar = **fail-open auth bypass**; bugünkü en kötü hâl fail-closed'dır.
4. **`sessions.revoked_at` UPDATE ile NULL'a çekilebilir** (kapanış denetçisi, canlı ölçüm: `tappa_app`
   olarak `UPDATE sessions SET revoked_at=NULL` → 1 satır). `tappa_app`'in tablo geneli UPDATE'i
   `last_used_at` için **gerekli**, sütun-düzeyi grant bu ayrımı ifade edemez. Gönderilen 5 sorgunun
   hiçbiri yapmıyor (`COALESCE` yalnız ileri yönlü) ve kural `db/queries/sessions.sql`'de yazılı:
   `revoked_at` NULL → non-NULL, asla geri. **Yapısal fix bir trigger'dır ve YENİ migration ister**
   (00003 immutable, §6) — M6/M7'de değerlendirilir. Şu an koruma dosya disiplininde, DB'de değil.
5. **`config.Load` `BaseURL`'ü doğrulamıyor** (2. kapsam genişlemesinden bilinçli kaçınıldı). `NewCookies`
   **prefix testi** yapar, URL parse etmez → başında boşluk olan veya URL olmayan `BaseURL` NOT-Secure
   dalına düşer (**non-prod'la sınırlı**; prod koşulsuz Secure). M5-03/M8 config sertleştirmesinde ele alınır.
6. **`TAPPA_ENV` YOKLUĞU hâlâ sessizce `dev`** — enum yalnız *yanlış* değeri reddeder, *eksik* olanı değil
   (kasıtlı varsayılan). TLS sonlandıran proxy arkasındaki prod'da operatör `TAPPA_ENV`'i unutur ve
   `TAPPA_BASE_URL`'i iç http adresinde bırakırsa çerez Secure'suz gider. Bugün sömürülebilir değil
   (`NewCookies`'in test dışı çağıranı yok). Kalan savunma **operasyoneldir** → M8 deploy denetimi.
7. **`DeviceInfo` sınırı UA'yı ENGELLEMEZ, yalnız sütunu sınırlar.** Bilinçli olarak **kısa** bir user
   agent bu sınırdan geçer. M5-02 `r.UserAgent()`'ı **doğrudan geçirmemeli** (§7: dış girdi handler
   sınırında doğrulanır) — kaba etiket türetmeli.

### M2-04'e devralınan not (M2-01 denetiminden, N3)
SV2 içindeki `ctr`'nin byte sırası ADR/skill'de açıkça sabitlenmedi (bilinçli) → **M2-04/M2-07
bilinen-cevap vektörleriyle** sabitlenmeli (little vs big-endian sessizce yanlış "makul" değer üretir).

### M6'ya devredilen (M1-11 denetiminden) — back-FK boşluğu
`transactions.entered_by` · `transaction_reviews.reviewer_id` · `audit_log.actor_id` →
`admin_users` FK'leri **eklenmedi** (00005 yorumları "M1-11'de eklenir" demişti — **artık
yanıltıcı**, 00005 immutable/düzeltilemez). M1-11 kartı bunları istemiyor + `reviewer_id NOT NULL`
FK'si rls_test fixture'ını (rastgele reviewer_id) kırardı. ⚠️ **2026-08-03: M6-01 BUNU YAPMADI ve
yapamazdı** — iki fazı da **yeni migration olmadan** sevk edildi (00011 A fazınındır ve `admin_users`'a
back-FK eklemez; B fazının `db/` diff'i **boş**). **Kalan tek sahip M6-04.** Eski hâliyle: **M6-04 (review akışı) / M6-01 (auth)**
bu back-FK'leri (composite same-tenant) + fixture yeniden yazımını yapar. Sınırlı risk: yazım
yolları henüz yok, reviewer_id self-review trigger'ıyla korunuyor. `actor_id` polimorfik
(admin|employee) → tek FK doğru değil, ayrı ele alınır.

### M6 handler denetimine not (M1-11'den)
`store.AdminUser.PasswordHash`/`TokenHash` üretilen struct'larda — handler'da `%+v`/slog ile
loglanırsa §7 sır sızıntısı. M6-01 handler denetiminde kontrol et.

### Devam eden düşük notlar
- **Dev-DB test kalıntısı birikiyor** (M3-02 security-auditor bulgusu): `internal/db/rls_test.go`
  random-UUID fixture'ları COMMIT ediyor ve `policy_versions`/`transactions` append-only + REVOKE DELETE
  olduğundan app-katmanı teardown **tasarımca imkânsız** (M1-09: imkânsızlık = garanti). Sonuç: her
  `make test` koşusu tenants/policies/... satırı ekliyor (auditor: tenants≈1089). Kırmızı çizgi DEĞİL,
  yalnız hijyen; demo/prod öncesi `make db-reset`. İstenirse owner-teardown veya testcontainers ile
  izole DB (M8 deploy denetimi) çözer.
- ⚠️ **2026-08-03: bu satırın `admin_sessions` yarısı ARTIK YANLIŞ.** 00011 (M6-01 A, `66d5442`)
  `admin_sessions_revocation_monotonic` trigger'ını **sevk etti** — `revoked_at` monoton, `tappa_owner`
  bile geri alamıyor — ve tablo-geneli UPDATE'i **REVOKE** edip yalnız `(last_used_at, revoked_at)`
  sütunlarına GRANT verdi. Ayrıca **`admin_sessions`'da `expires_at` sütunu HİÇ YOK** (kolonlar: id,
  tenant_id, admin_user_id, token_hash, created_at, last_used_at, revoked_at) — o alan
  `password_resets`'e ait. **Sağ kalan doğru yarı:** `password_resets.used_at`/`expires_at` gerçekten
  hâlâ serbest. ⚠️ Ve **daha güçlü kill switch'in hiçbir monotonluk koruması yok**: `admin_users.status`
  tek satırla o admin'in **tüm** oturumlarını öldürüyor, ama `SET status='active'` hepsini geri getiriyor
  (ölçüldü). Eski hâliyle: `password_resets.used_at`/`admin_sessions.revoked_at`/`expires_at` UPDATE-edilebilir (append-only
  trigger yok); tek-kullanımlık/iptal bütünlüğü **app katmanında** (M6/M7 sorguları). sessions.revoked_at
  ile aynı desen; istenirse M6'da immutability trigger'ı defense-in-depth eklenebilir.
- **aes_key_ref KEK-sarmalı doğrulaması** (M1-05'ten): şema bytea zorlayamaz → insert-yolu (M2/M5)
  + seed KEK-sarmalı bekler; KEK DB dışında (config `TAPPA_TAG_KEK`) — M8 deploy denetimi.

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

### ⏳ Bekleyen kullanıcı eylemleri → **[docs/backlog.md](../backlog.md)**

Kullanıcının yapabileceği (ajanın kodla kapatamayacağı) işler artık **tek yerde**:
[docs/backlog.md](../backlog.md). Buraya kopyalama — çelişirler.

- **B1 — iOS Safari çerez ömrü ölçümü (Q11).** Gerçek iPhone ister. **Hiçbir şeyi
  bloklamıyor:** M5-01 sunucu tarafı ölçümden bağımsız, bu yüzden kabul kriteri olamadı
  (kart düzeltmesi 2026-07-31). Sonuç `open-questions.md` → Q11'e yazılır.
- **B2 — arm64 Go kurulumu (Q26).** `sudo` ister. **Hiçbir şeyi bloklamıyor** — her şey
  amd64 Go 1.26.5 ile yeşil; kazanç yalnız yerel derleme/test hızı.

**Kullanıcı "backlog ekle" derse madde oraya yazılır.**

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
| `git status --short` | temiz (görev arasındaysan) |
| `ls .env` | var (git'e **girmez**) |
| `docker compose ps` | `tappa-db` ayakta ve `healthy` |
| `make migrate-status` | **00001–00011 uygulanmış** (00011 = M6-01 A fazı, admin çözümleme) |
| `make check` | **exit 0** — ama yalnız **temiz ağaçta** (aşağı) |
| `make test` | **14 paket** `ok`, **PASS 1647 / SKIP 0 / FAIL 0** (M6-02 sonrası) · **gözlenen aralık 92–150 sn** (makine durumuna göre; **hedef değil, gözlem kaydı**) · sayım: `make test GOFLAGS=-v \| grep -c -- '--- PASS:'` · ⚠️ çıplak `go test` DB testlerini **sessizce SKIP eder** (ölçüldü: PASS 1318 / SKIP 214) |
| `make test-short` | **gözlenen aralık 51–74 sn**, **TAM 3 SKIP** (`TestAuthenticate_TimingIsFlat`, `TestPanelE2E_TimingIsFlatOverHTTP`, `TestSeedDB_ADayAtKFStJulians`) — iç döngü içindir, **commit öncesi `make test`**. ⚠️ Bu bant **üç kez dar yazılıp üç kez tutmadı**; artık gözlenen aralık ve **hedef değil** |
| **⚠️ İki bilinen flake** | İkisi de **M6-01 kaynaklı DEĞİL**, ikisi de **önceden var**: (1) `TestPolicySetDB_ConcurrentFirstTapsMaterialiseOnce` — M7-03 devrinin (`EnsureBaselinePolicy` eşzamanlılıkta 23505) test yüzü, ~26 koşuda 2; son 8 kapanış koşusunda **0**. (2) **bağlantı tükenmesi** (`FATAL: sorry, too many clients already`) — `max_connections=100` − 3 rezerve = **97**, ve `internal/db` + `internal/sun`'ın kırmızı-çizgi yarış testleri **54'er** bağlantı açıyor = **108 > 97**, yani **tek başlarına** sınırı aşıyorlar. Goroutine sayısını düşürmek bir **§4.4 testini zayıflatır** → düzeltilmedi. **Sonuç: `make check` yeşilliği bu iki testin zamanlamasına bağlı; kırmızı görürsen ÖNCE hangisi olduğuna bak.** |
| `make simulate-day` | KF St Julians'ta bir gün: `PASS`, ~64 sn (~62'si ADR 0006 beklemesi). **`make seed` yapılmış olmalı** |

⚠️ **`make check` son adımı `git diff --exit-code`.** Commit edilmemiş iş varken **exit 2** verir ve bu
**bilgi taşımaz** — fmt/lint/test geçmiş olabilir. Bir ajan *"make check kırmızı"* diyorsa hangi adımın
düştüğünü söylemeli; iş bitmiş sayılmadan önce **commit sonrası** exit 0 görülmeli.

⚠️ **`make test` kullan, çıplak `go test` DEĞİL.** Makefile `.env`'i yüklüyor
(`-include .env` + `export`); çıplak `go test ./...` `DATABASE_URL` olmadığı için **her DB testini
sessizce SKIP eder** ve §4.4/§4.5/§4.6 hakkında hiçbir şey kanıtlamadan yeşil verir (M5-05 denetimi).
Bir iddia "N test geçti, 0 SKIP" diyorsa **hangi komutla** ölçüldüğünü söylemeli. M5-08'de bir mutasyon
kolu çıplak koşuda **"ok"** verdi (2,5 sn), `make test` ile **24,6 sn** ve gerçek sonuç.

⚠️ **Dev-DB birikimi artık her `make test` koşusunda ~44 satır.** M5-08'in 20 002 satırlık kalıntısı
`make db-reset` ile temizlendi (2026-08-02), ama gün simülasyonu her koşuda **31 `transactions` + 11
`employees` + 2 `audit_log`** ekliyor ve bunlar §4.3/§4.6 gereği **silinemez** (kusur değil, garanti).
Üretilen çalışanlar **koşum damgalı** (`Maria Borg [sim 08-02T00:40:24 f4ef]`) → seed'li kadroyla
karışmıyor; ama damga `full_name` üzerinden **kullanıcıya görünen yüzeye** (aktivasyon ekranı, M6
dashboard) sızar, yani bugünkü dev-DB **ekran görüntüsüne hazır değil**. Demo öncesi `make db-reset`.
**CI etkilenmiyor** — workflow `make migrate`/`make seed` koşmuyor (aşağı: bu yüzden 140 test CI'da
FAIL ederdi; **önceden var olan** durum, M5-09'un gerilemesi değil, sahibi **M8**).

**Zorunlu env değişkenleri** (eksikse başlangıçta panic — bilinçli, §config): `DATABASE_URL` ·
`DATABASE_MIGRATE_URL` (farklı olmalı) · `TAPPA_SESSION_HMAC_KEY` · **`TAPPA_INVITE_HMAC_KEY`**
(M5-02; oturum anahtarıyla **aynı olamaz**) · `TAPPA_TAG_KEK` · **`TAPPA_RETENTION_YEARS`** (M5-02) ·
`TAPPA_ENV` ∈ {dev, staging, prod} · `TAPPA_TRUSTED_PROXIES` (varsayılan rota **prod'da hata**).

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
| M1-06 | Migration 0005: transactions (append-only) & audit_log | **done** | `d91c609` · **iki denetçi ONAY** (kırmızı çizgi ihlali yok) · immutability kuşak+kemer (REVOKE UPDATE,DELETE + `tappa_forbid_mutation` trigger; superuser DISABLE-trigger sınırı kabul) · §4.6 nullable id + CHECK (flag/manuel/reject kaydedilebilir) · **`UNIQUE(tag_uid,ctr)` yok** · transaction_reviews 3 kısıt + çapraz-tenant review **yapısal** composite FK ile kapalı (X3/X4 kanıtı) · reviewer_id/entered_by FK M1-11'e ertelendi |
| M1-07 | internal/db: havuz ve tenant kapsamlı transaction | **done** | `f73972a` · üçüncü göz **1. turda** ONAY (3 negatif kontrolle: set_config true→false, rollback→commit, panic silme — üçü de testi kırmızıya döndürdü) · `WithTenant` `set_config(...,$1,true)` param-bağlı, çıplak SET yok · havuz unexported (yapısal kapalı) · uuid.Nil guard · 5 gerçek-Postgres -race test (aynı-backend sızıntı-yok kanıtı) · **imza sapması:** callback `pgx.Tx` (store M1-08'de) — kart düzeltildi · resolve erişimi + go.mod'a templ geri-dönüşü M1-08'e/M2'ye |
| M1-08 | İlk sqlc sorguları | **done** | `62b70a8` · **iki denetçi ONAY** · `make gen`/`build`/`dev` **yeşil** (planlı sqlc kırmızısı bitti) · 6 tenant-kapsamlı sorgu (hepsi açık tenant_id) · `AdvanceTagCounter` atomik CTE strict-`<` (canlı + 2-goroutine -race) · **resolve lookups ELLE** (`internal/db/resolve.go` — sqlc `RETURNS TABLE`'ı tipleyemedi; yalnız SECURITY DEFINER fonksiyon çağırır) · Q25(c) cidr[] override **gerekmedi** (pgx varsayılanı) · WithTenant pgx.Tx kaldı |
| M1-09 | RLS izolasyonu ve değişmezlik testleri | **done** | `a033c8a` · üçüncü göz ONAY — **non-vacuous 3 bağımsız yolla kanıtlandı** (RLS DISABLE, trigger DISABLE, kaynak mutasyonu → hepsi RED, geri alındı) · 7 vaka + 9 tablo · M0-03 kaçış yolları kapalı (ham SQL, pozitif kontrol, çalışma-anı rol) · `TestResolveColumns_MatchSchema` drift koruması · **2 sapma çözüldü:** x/text CVE yamalandı (`1554135`), redline R3/R5 `_test.go` muafiyeti + test sadeleştirildi (`<sonraki>`) |
| M1-10 | Seed verisi ve sabit ID'ler | **done** | `516be65` · üçüncü göz ONAY · KF 9 lokasyon + KM 5 departman, 36 çalışan, 12 tag · idempotent (2. koşu INSERT 0 0) · 12/12 sahte-etiketli anahtar (§4.7) · doküman IP cidr[] · Malta GPS min 783.6m · çift-uçlu vardiya · cross-tenant paylaşım 0 · ids.go 53 UUID+12 tag DB ile birebir · yalnız master veri (admin owner M1-11'e) |
| M1-11 | Migration 0006: admin kullanıcıları | **done** | `f416d45` · **iki denetçi ONAY** (kırmızı çizgi ihlali yok) · 3 tablo RLS beşlisi + REVOKE DELETE + composite same-tenant FK · **admin'de resolver YOK** (tenant login'de bilinir) · admin_sessions employee sessions'tan ayrı · Q03 bcrypt `password_hash text` (x/crypto M6-01'de) · seed admin owner (dev-only bcrypt) · rls_test +3 tablo (non-vacuous) · **back-FK'ler M6'ya ertelendi** (aşağı) |

### M2 — [SUN doğrulama](m2-sun.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M2-01 | ADR 0003: SDM modu ve anahtar yönetimi | **done** | `5a9cd2e` · üçüncü göz ONAY · **Q05 plain SDM + Q06 per-tag random** normatif · plain URL (`tag`/`ctr` big-endian/`cmac`) · KEK AES-256-GCM, `aes_key_ref`=nonce(12)‖ct(16)‖tag(16)=44B · **AAD=ham 7-byte UID v1'de ZORUNLU** (denetçi bulgusu: tappa_app UPDATE→sarmalı anahtar taşınabilir; sıfır maliyet pre-prod) · ctr-wrap fail-closed · AN12196/NT4H2421Gx ref |
| M2-02 | AES-CMAC (RFC 4493) | **done** | `2380baa` · üçüncü göz ONAY (RFC vektörleri **OpenSSL ile bağımsız yeniden hesaplandı**, mutasyonla non-vacuous) · kurum-içi `crypto/aes`, **dep yok** · 4 resmi vektör + K1/K2 + padding · **%100 kapsam** · kısaltma yok (M2-04) · `cmac(key,msg)([16]byte,error)` |
| M2-03 | SDM URL ayrıştırma | **done** | `ac51b20` · üçüncü göz ONAY · `Parse`→`Params{UID(kanonik BÜYÜK), UIDBytes, Ctr(big-endian), CMAC, Channel}`+`HasSUN()` · **mixed-case silent-zero-row tuzağı kapatıldı** (DB sondasıyla) · QR→sun_valid=false · fuzz 10.9M exec panik yok · §4.7 jenerik/sır-siz hata (mutasyonla) · yeni dep yok |
| M2-04 | Oturum anahtarı, kısaltılmış MAC, sabit zamanlı karşılaştırma | **done** | `88c6036` · **iki denetçi ONAY** (1. tur RED: SV2 sayaç byte'ları URL'ye göre TERS'ti → yapısal düzeltildi, `sv2()` ham `ctrBytes` verbatim) · tek-indeksli 8-byte kısaltma · `ConstantTimeCompare` (R7) · golden bağımsız Python'la doğrulandı · %98.9 kapsam · **değer-endian M2-07'ye ertelendi** (ayrı eksen) |
| M2-05 | Anahtar sarmalama (KEK) | **done** | `0d23d30` · **iki denetçi ONAY** · `Wrap(kek,uid,key)`/`Unwrap(kek,uid,ref)`+`Zero()` AES-256-GCM · AAD=UID taşınabilirlik-koruması (uidA→uidB unwrap hata) · 44-byte düzen · **KEK parametre (cache yok)** · AES-256 zorlanıyor (downgrade önlenir) · düz-anahtar/KEK sızmaz (mutasyonla) · %96.1 kapsam |
| M2-06 | Atomik sayaç ilerletme ve eşzamanlılık testi | **done** | `2092796` · **iki denetçi ONAY** (§4.4 en kritik) · `sun.AdvanceCounter` M1-08 atomik CTE'sini kullanır (verify'dan ayrı) · **50-goroutine `-race` → tam 1 kazanan** (her iki denetçi kendi koştu) · **negatif kontrol yeniden üretildi** (TOCTOU→50 kazanan) · strict `<`, 0-satır→ErrReplay, gömülü eşik yok, R4 temiz · %96.3 kapsam |
| M2-07 | sun.Verify ve test vektörleri | **done** | `cd639f5` · **iki denetçi ONAY** · `Verify` tüm zinciri birleştirir (resolve→retired/lost→QR→unwrap+verifyMAC+Zero→**sonra** advance) · `Result` döner **verdict vermez** · vaka tablosu tam + N-goroutine tam-1 (`-race`) · sıra kanıtı (kötü CMAC→advance yok) · §4.7 no-leak mutasyonla · %96.5 kapsam · **self-consistent vektör** (gerçek çip M8-05'te) |

### M3 — [Policy motoru](m3-policy-motoru.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M3-01 | ADR 0004: policy motoru modeli | **done** | `01c7a8a` · üçüncü göz **1. turda** ONAY · `docs/adr/0004-policy-motoru-modeli.md` (413 satır, 0002/0003 iskeleti) · 7+3 içerik maddesi gerekçeli · **§5 satır 1–5↔guardrail, 6–7↔baseline** hem tablo hem düz metin (denetçi CLAUDE.md §5'i satır satır doğruladı) · 5 effect, 2 varsayılan (tap:*→review / authz→deny), guardrail sırası + 2 somut sömürü, ignore/redirect tenant'a kapalı, Y-K spesifik-ezer, 4 alternatif · biyometrik anahtar YOK (§4.1), §4 gevşetme yok · **2 bloklamayan gözlem M3-04'e devredildi** (kart `redirect` eksik, eval-time bilinmeyen operatör) |
| M3-02 | Policy şeması (append-only sürümler) | **done** | `4126e4c` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi ihlali yok) · migration 00007: policies + policy_versions (**append-only**) + policy_attachments, üçünde RLS beşlisi (birebir NULLIF USING+WITH CHECK, pg_policies'ten okundu) · §4.3 kuşak+kemer non-vacuous (trigger DISABLE→superuser UPDATE başarılı → koruma REVOKE değil paylaşılan `tappa_forbid_mutation` trigger) · **`layer` CHECK `guardrail`'i reddediyor** (23514 — guardrail DB'ye yazılamaz) · composite same-tenant FK çapraz-tenant'ı blokluyor (23503) · `tappa_app` rolsuper=f/rolbypassrls=f teyit · **2 sapma kabul:** `policies` DELETE REVOKE (§4.6 enabled durum alanı; planlı silme yolu yok), `created_by` FK-siz uuid (admin FK M6/M7'ye, M1-11 kalıbı) · rls_test.go +3 tablo non-vacuous · models.go make gen additive · make check/gen/audit yeşil |
| M3-03 | Belge modeli, ayrıştırma ve doğrulama | **done** | `555e1c5` · üçüncü göz **1. turda** ONAY (non-vacuous **2 mutasyonla** kanıtlandı: sys: no-op→test RED, documentEffect→true→test RED) · `internal/policy/{document,validate}.go`+testler, **%98.8 kapsam** · bilinmeyen effect/action/operatör/anahtar→hata (+ `DisallowUnknownFields` typo yakalama), sys: rezerve (case-insensitive, iki katman), ignore/redirect belgede reddedilir, nicel DoS sınırları (byte/ifade/action/resource/condition/IpInPrefix + `CheckTenantQuota` doc+version, sabitler tek yerde), bozuk JSON+fuzz (456K exec crasher yok), §4.7 hata değeri sızmıyor · saf paket (Evaluate M3-04'e bırakıldı) · ADR listeleri birebir (10 operatör/7 eylem/24 anahtar/5 effect) · **bounded-param wiring boş → M3-05'e devir** (aşağı) |
| M3-04 | Değerlendirici (koşullar, öncelik, açıklanabilirlik) | **done** | `de831e1` · üçüncü göz **1. turda** ONAY (non-vacuous **3 mutasyonla**: guardrail return kaldır→terminal RED, deny/review takas→restrictiveness RED, bilinmeyen-op matched=false kaldır→deny koşulsuzlaştı 4 test RED) · `internal/policy/{evaluate,conditions}.go`, **%97.9 kapsam** (evaluate.go %100) · saf `Evaluate(Set,Context) Decision` · guardrail sıralı+**terminal** (alt katman OnAnomaly çağırmıyor=hiç çalışmıyor kanıtı) · en-kısıtlayıcı-kazanır + spesifik-resource tie-break · varsayılan `tap:record`→review / diğer 6 (tap:approve dahil)→deny · **bilinmeyen-op deny'yi koşulsuzlaştırMIYOR** · eksik-anahtar≠false (StringNotEquals dahil) · determinizm 1000-koşu (map-sıra bağımsız) · anomaly injectable sink+slog fallback §4.7-temiz · **2 kart düzeltmesi** (redirect eksiği + tap:approve→deny ADR §3, denetçi doğruladı) · Context struct sapması gerekçeli · 2 bloklamayan not (Türkçe yorum→M3-05, default Layer=guardrail→M3-07) |
| M3-05 | Guardrail politikaları | **done** | `e51504b` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, **§4 en kritik**, kırmızı çizgi ihlali yok) · non-vacuous **3 mutasyonla** (deactivated'ı öne al→sıra+R8 leak RED; sun-invalid Match→false→R8 RED; config üst sınır kaldır→20000000 RED) · `internal/policy/guardrails.go` 10 `sys:*` guardrail TEK sıralı slice, kodda gömülü, devre-dışı API YOK · **R8 sıra** sun-invalid(3)<deactivated(7)<debounce(8) — üçü eşleşince sun-invalid kazanır + SecurityAlert BOŞ (sızıntı/push-seli/replay kapalı) · terminallik: geniş tenant allow guardrail deny'ini çeviremiyor · tenant-mismatch→redirect+kayıt-yok · person-debounce KİŞİ bazlı (nil gap→kayıt düşmez §4.6) · Context 4 tipli sunucu-alanı (belge sözlüğü dışı) · SecurityAlert sabit sözlük §4.7-temiz · **config aralık** GPS 25–1000/debounce 30–300 başlangıçta (20000000+GPS=5 reddedilir), guardrail+config tek kaynak · bounded-param 3 anahtar (occurredAtSkew dahil) · policy %98.2 · **N1/N2/N3 → M4/M5 devir** (aşağı) |
| M3-06 | Tappa Baseline yönetilen politikası | **done** | `a9b4dc6` · üçüncü göz **1. turda** ONAY (non-vacuous **3 mutasyonla**: no-evidence effect değiş→RED; base: rezerv no-op→RED; owner'dan policy:edit çıkar→owner default deny=**fail-closed lockout gerçek** kanıtı) · `internal/policy/baseline.go` 8 `base:*` tap ifadesi + **2 yetki ifadesi** (authz-owner=6 eylem, authz-manager=4 eylem alt kümesi) · fail-closed lockout önleniyor (owner policy:edit baseline allow — guardrail owner'da ateşlemez) · **base: rezerv** validate.go'ya eklendi (tenant layer, case-insensitive) · base:ctr-gap-review kaynak-kapsamlı + tenant override (specExact>specType) · guardrail dokunulmaz (allow-all tenant→retired/deactivated guardrail deny kazanır) · ignore/redirect yok · BaselineVersion + otomatik-güncelleme-yok · **DB yazma M3-06'da YOK** (kanonik kaynak, M7-03 materyalize) · rol modeli admin_users {owner,manager} teyit · baseline.go %100/policy %98.3 · **manager employee:deactivate: kullanıcı kararı = manager DA yapabilir** (`a6c41dd` followup, odaklı üçüncü göz ONAY; policy:edit owner-only kaldı) |
| M3-07 | Kararın kayda bağlanması | **done** | `1f144b7` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, §4.3 kırmızı çizgi ihlali yok) · migration 00008: transactions'a `policy_version_id`/`matched_sid`/`policy_layer`/`policy_context jsonb` (uygulanmış migration değişmedi) · **§4.6 kritik doğrulandı:** consistency CHECK Evaluate'in HER meşru kararını kabul eder (baseline `&vid` daima non-nil), yalnız wiring-bug keser (verdict CHECK precedent'i) — kayıt kaybı yok · §4.3 yeni sütunlar immutable (belt1 REVOKE sütun-seviyesi f + belt2 trigger DISABLE→superuser UPDATE başarılı kanıtı) · composite same-tenant FK policy_versions'a (23503 çapraz-tenant) + **ON DELETE RESTRICT** (cited version silinemez, delil zinciri) + policy_versions UNIQUE(id,tenant_id) hedefi · §4.7 policy_context mesafe/ham-koordinat değil · sqlc InsertTransaction+2 read additive (hepsi Transaction döner) · make check/gen/audit yeşil · **N4 → M5-05 devir** (Decision→sütun sadakati, aşağı) |
| M3-08 | Test seti ve gevşetilemezlik kanıtı | **done** | `c39ccae` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, guardrail bypass + sys: sızıntısı arandı, bulunamadı) · `internal/policy/{property,invariants}_test.go` (üretim kodu DEĞİŞMEDİ) · **özellik testi** `TestGuardrail_NoTenantPolicyCanLoosen` fixed-seed 2000 iter: hiçbir rastgele tenant/baseline politikası guardrail deny/ignore/redirect'i allow'a çeviremez · **non-vacuous** (iterasyon-başına guardrail-siz kontrol allow assert eder; üçüncü göz katman sırasını bozunca step-3 property RED) · security-auditor bağımsız 7-guardrail bypass sondası (en spesifik resource dahil hepsi tuttu) · **invariant testleri:** §4.6 kanıt-yok→review (2 yığın), §4.1 yüzey-kilidi (24 anahtar+8 Context alanı; key+field ekleme→RED; D1 denylist değil çünkü redline R1 _test.go tarar), guardrail-restrictive-only · §4.7 test hata mesajı yalnız anahtar-adı · kapsam %98.3 |
| M3-09 | ADR 0005: kabul edilen riskler | **done** | `0c0feb4` · üçüncü göz **1. turda** ONAY (12 kabul kriteri) · `docs/adr/0005-kabul-edilen-riskler.md` — 6 risk (buddy punching A4/Q19, sahte GPS A3, URL biriktirme A1/Q21, mekânda proxy Y-E, müdürün kimlik basması Y-D, plaket devri) her biri neden+tespit sinyali+görev+satış · **referanslanan 8 sid + 2 anahtar kodda GERÇEK** (denetçi grep'ledi: base:ctr-gap-review/gps-conflict-review/no-evidence-review, sys:tag-not-active/tenant-mismatch/tap-freshness/occurred-at-bound) · handoff §2 tutarlı (parmak izi=yalnız buddy punching) · mekânda-proxy uyarısı iki yönlü · append kuralı + §4.1 sınırı · "ileride bakarız" yok |

### M4 — [Tap karar motoru](m4-tap-motoru.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M4-01 | internal/geo: haversine ve yarıçap | **done** | `f791f91` · üçüncü göz **1. turda** ONAY · `internal/geo` saf paket (yalnız `math`) — `Point{Lat,Lng}`, `Distance` (haversine, R=6371008.8, **atan2** → acos-NaN tuzağı yapısal yok), `WithinRadius(a,b,radiusM)` **strict `<`** (§5 satır 6 "GPS < 150 m" ile hizalı, 150 m DIŞARIDA) · yarıçap **parametre** (config besler, gömülü değil) · **denetçi mesafeleri BAĞIMSIZ yeniden hesapladı** (783.557/1115.594/0/π·R byte-identical) · lat/lng-swap + %100 kapsam mutasyonla RED · §4.7 koordinat loglanmıyor (config/policy import yok, döngü yok) |
| M4-02 | Karar girdi/çıktı tipleri | **done** | `860fcd8` · üçüncü göz **1. turda** ONAY · `internal/domain/tap/{types,decide}.go` — `Input` (14 alan) + `Decision` (9 alan) karta birebir + `Decide(Input) Decision` imzası (gövde M4-03 **panic-stub**, zero-value §4.6 sessiz-onay riski yok) · **saf** (kendi import'ları `net/netip,time,geo,uuid`; store/db/sun/sql/http/pgx KODDA yok; `time.Now()` çağrısı yok; math/rand+database/sql/driver yalnız uuid'den, policy ile birebir) · enum'lar typed (DB CHECK sözlükleriyle birebir) · **`Employee` pointer (§5.3 nil=oturum yok) + Status (§5.4 deactivated) ayrı** · tap kendi `SUNResult`'ı (sun.Result db/store sürüklüyor) · sapma: `Employee.ActivatedAt` (Practice sunucu-türetim kaynağı, §5/M4-06 exploit önler) |
| M4-03 | Decide(): bağlam kurma ve kararın uygulanması | **done** | `bfbbf77` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor R8, §4.6/§5, kırmızı çizgi ihlali yok) · `Decide` Input→policy.Context kurar (ipMatch/gpsMatch/gpsDistanceM/gpsConflict/ctrGap/sunValid/channel/tag/employee/location) → `policy.Evaluate` **tek çağrı** (if-zinciri/erken-return YOK) → effect→verdict; **no-session→redirect+kayıt-yok** (§5.3 tek istisna); row7→flag (asla reject); boş set→flag · **R8:** deactivated+invalid-SUN→sun-invalid+Security=false (üçüncü göz erken-return mutasyonuyla, security-auditor kod-okumasıyla) · **marker-hilesi iki yönlü doğrulandı** (SessionTenantID=Employee!=nil işareti→sys:no-session; TagTenantID nil→sys:tenant-mismatch inert) · §4.7 mesafe/ham-koordinat değil · saf (tap→policy/geo, store/db/sun yok) · %95.7 · **PolicySet Input alanı + Decision explainability alanları (M4-02 kart düzeltildi)** · **🔴 N5→M5 bloklayan tenant devri** (aşağı) |
| M4-04 | Yön tayini (in/out) | **done** | `703d3d1` · üçüncü göz **1. turda** ONAY (**4 mutasyon** öldürüldü: toggle-ters/stale-not/practice-guard/Type-yay) · `Decide` `Decision.Type` saf toggle (LastOpenIn varsa out, yoksa in) · **takvim-günü filtresi YOK** (bağımsız cross-midnight/ay/yıl/artık-gün + 5h fark 400 gün sabit; Rusty Bar 18:05→02:10 out) · stale **>18h** (strict) → out+note (asla sessiz in) · **practice LastOpenIn → in muamelesi** (eğitim tap'i gerçek check-in'i açık tutamaz, M4-06 saat-şişirme) · Type yalnız ok/flag (reject/ignored/redirect→nil) · UTC saf süre, sabit-Now determinizmi · saf (time.Now yok) · %95.1 |
| M4-05 | Vardiya çözümü ve geç kalma | **done** | `63f6b4a` · üçüncü göz **1. turda** ONAY · **DST bağımsız yeniden hesaplandı** (denetçi Python `zoneinfo`: mart 09:15→15 geç, ekim 09:20→20 geç, overnight 01:00→420; naif midnight+offset bug −45/80 mutasyonla yakalandı) · `time.LoadLocation("Europe/Malta")` (sabit ofset yok), **tzdata tap paketine gömülü** (tek binary) · **geç kalma RAPOR-only** `Decision.MinutesLate *int` (nil=hesaplanmadı, int dakika **float yok** §6, Evaluate SONRASI, context'e girmiyor, hiçbir baseline time:minutesLate okumuyor → 180-geç→OK) · yalnız check-IN'de · çapraz-lokasyon Q17 (`employee:crossLocation`→base:cross-location-note + `Decision.CrossLocation`, geç damgası yok) · Shift==nil VE boş-tz→nil (LoadLocation("")→UTC tuzağı guard'lı) · %96.4 · cmd/tappa dokunulmadı |
| M4-06 | Trust puanı, QR kanalı, practice tap | **done** | `a82dfa8` · üçüncü göz **1. turda** ONAY (2 mutasyon: trustScore sabit / isPracticeTap false → RED) · Trust `20+50(IP)+30(GPS)` verdict switch ÖNCESİ, **verdict'ten bağımsız** (reject 70 > ok 50) · **Practice sunucu-türev** (`ActivatedAt`+`LastForPerson==nil`, ok/flag'te) — **client alanı YOK** (reflection guard `TestInput_HasNoClientPracticeField` yeni alanı yakalıyor), checkout asla practice → **saat-şişirme exploit'i yapısal kapalı** · QR uçtan uca (base:qr-requires-ip): QR+IP-yok+**GPS-var→flag** (Q15), QR+IP→ok, SUN-suz QR sys:sun-invalid'e takılmaz (NFC-only) · manuel SUN atlar; entered_by M5-05 yazma-yoluna ertelendi (kart+M5-05 kriteri eklendi, Decide saf func hata dönemez) · %96.7 · **sertleştirme notu: isPracticeTap'e LastOpenIn==nil (savunma-derinliği, client-erişilemez)→M4-07** |
| M4-07 | Tablo bazlı test seti | **done** | `c5536be` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor R6/R8) · `table_test.go` duplikasyon-ledger'ı: §5 yedi satır (`TestDecide_Section5Rows`) + 5 zorunlu ek vaka · **debounce KİŞİ-bazlı** vaka (farklı kişiler aynı plaket 10sn→hepsi ok) — person-scoping mutasyonuyla RED kanıtlandı (merkezi) · mobil-veri (ok/trust 50/not "verified via GPS" baseline'dan) · Rusty Bar gece turu cross-midnight · deaktive→reject+Security · **R8** Evaluate tek çağrı erken-return yok (redline temiz), **R6** row7-flag/no-session-tek-redirect/default→flag · **isPracticeTap sertleştirmesi** (+`LastOpenIn==nil`, revert→RED, kayıt yazımını etkilemez §4.6) · %96.7 kapsam · guardrails.go:222 yorum-notu→internal/policy sonraki dokunuş |

### M5 — [Tap akışı](m5-tap-akisi.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M5-01 | internal/session: oturum yaşam döngüsü | **done** | `a71e1b2` · **iki denetçi ONAY** (genel üçüncü göz **3. turda** + `tappa-security-auditor` kapanış turu) · **5 tur, 2 RED, ikisi de aynı sınıf: "yorum, kodun sağlamadığı garantiyi beyan ediyor"** · **RED-1** `Token` unexported alanda `%v/%+v/%#v/slog` ile ham token bastı (`fmt`, `CanInterface()==false` olunca `Formatter/Stringer/LogValuer` **atlar**) → `struct{ v *string }`; kapanış denetçisi **17 taşıyıcı × 18 render = 306 ölçüm, 0 sızıntı** + pozitif kontrol (çıplak `string` 18'in 13'ünde sızdı) · **RED-2** `Cookies` sıfır değeri (`var c`, `Cookies{}`, yazılmamış struct alanı — Go'da yasak olan alanı **adlandırmaktır**) prod'da `Set` **ve** `Clear`'da Secure'suz çerez yazdı → kutup çevrildi `struct{ insecure bool }`; 3. tur denetçisi **17 sıfır-değer yolu + 99 Env×BaseURL** kombinasyonunu yenemedi · `Verify` **tek sorgu** gerçek Postgres'te iki yolla ölçüldü (`pg_stat_user_tables` Δ + `log_statement=all`) · RLS izolasyonu **non-vacuous** (3 denetçi ayrı ayrı `DISABLE ROW LEVEL SECURITY` → RED → geri) · 5 sorguda açık `tenant_id`, DELETE yetkisi `f` · §5 satır 3/4 **korundu**: `Verify` iptalde **dolu `Resolved` + `ErrRevoked`** · sqlc çıktısı bağımsız yeniden üretilip **bayt bayt** eşleşti · kapsam **%94.0** (`deviceLabel`, `NewCookies`, `Secure` %100) · **kapsam genişlemesi:** `TAPPA_ENV` kapalı küme (`internal/config`) · **migration YOK** (00003 zaten var) · **Q11 AÇIK** (gerçek iPhone — yukarı) · 7 devir → "M5-02/M5-03'e devralınan" |
| M5-02 | Davet ve aktivasyon akışı | **done** | **A fazı** `9139ee7` · **B fazı** `0601b6d` · **iki fazda toplam 5 denetim turu, 3 RED** · **A:** `employee_invites` (RLS beşlisi, `password_resets` kalıbı) + `resolve_invite_by_code_hash` (ADR 0002 md.7 **üçüncü** resolver) + **kaynaşık CTE `ConsumeInviteAndActivate`** (iki ayrı sorgu "aktive ⇒ davet tüketildi"i çağrı sırasına bırakıyordu = hayalet-çalışan) + iç CTE'de **`EXISTS` guard'ı** (veri-değiştiren CTE koşulsuz çalışır → deaktif çalışanda davet **yanıyordu**; COMMIT ile ölçüldü, guard'la `burned=f`) + **sütun-düzeyi `GRANT UPDATE (used_at)`** (diriltme/kaydırma/hash-yeniden-yazma üçü de `permission denied`) · **A-RED:** kart CHECK'in **kod entropisini zorladığını** sanıyordu — `sha256('123456')` da 64-hex, tel-tuzak hiç ateşlenmiyordu → yükümlülük **Kabul kriterleri**ne taşındı · `FOR SHARE` **ölçümle reddedildi** (40P01 deadlock, iki cihaz aynı çalışanı aktive ederken) · **B:** `internal/{invite,audit,handler}` + ilk `.templ` sayfaları + `00010 locations.wifi_ssid` · **alan ayrımı çift** (`TAPPA_INVITE_HMAC_KEY` + etiketli girdi; aynı anahtar altında bile session yapısından farklı, ölçüldü) · **B-RED 1: aktivasyon-fixation** (SameSite çerez **yazmayı** kısıtlamaz → çapraz-site GET saldırganın kodunu ekiyor, sonraki GET **başka tenant'ın** formunu render ediyor, `Submit` mevcut oturumu görmediği için kurbanın oturumu **sessizce eziliyordu**) → CSRF token + 409 + koşullu ekim-reddi, **5 mutasyonla** kanıtlandı · **B-RED 2: sınırsız SİLİNEMEZ `audit_log` yazımı** (300 istek → 290×429 ama **300 satır**; `audit_log` append-only, `tappa_owner` bile silemiyor → tek ölü davet linkiyle izin şişirilmesi; §4.6'nın koruduğu iz kendi bağışıklığıyla silah oluyordu) → **üç bütçe** (flood/unknown/invite), 300→**11 satır** · **M5-01'in RED'i yeni pakette yeniden üretilmişti** (`activationState.code` çıplak `string`, `%+v` ham kodu basıyordu) → `invite.Code` · `heldBy` **fail-closed** (DB hatası "oturum yok" değil "bilmiyorum") · GDPR Art.13 **config'den** render (koda hukuki sayı gömülmedi, Q13→backlog B3) · **davet üreten HTTP uç noktası YOK** (admin auth M6; kimliksiz uç nokta Y-D'yi genişletirdi) · **7 aşırı iddia ölçümle çürütülüp indirildi** · **§5 satır 3 BAĞLANMADI** (hedef var, yönlendirme M5-04) · handler+invite+audit **64 test**, 0 SKIP · Node yok |
| M5-03 | Middleware: gerçek IP, tenant, oran sınırı | **done** | `1fdd1ad` · **iki denetçi**, **2 RED** (üçüncü göz + `tappa-security-auditor`) · `internal/httpx/{realip,identity,ratelimit}.go` · **XFF SAĞDAN sola**, tüm başlık örnekleri, güvenilmeyen peer → başlık **hiç okunmaz**, `TrustedProxies` boş → `RemoteAddr`; chi `RealIP` **kullanılmadı** (koşulsuz güvenir; §5'te IP = **50 puan**, sahtelenebilir adres hiç adres olmamasından **kötü**) · **RED-1:** *"tek otorite / handler kazara ham başlığa uzanamaz"* yanlıştı — `Forwarded` (RFC 7239)/`CF-Connecting-IP`/`X-Client-IP` handler'a ulaşıyordu → strip listesi **3→32**, **canlı TCP soketiyle** ölçüldü (36 adayın **23'ü** geçiyordu → **4**), iddia denylist kapsamına indirildi; kalan 4 **pozitif kontrol** (`Via`, `X-Forwarded-Host/-Proto` adres değil; **`Origin` CSRF için taşıyıcı**, silinse aktivasyon kırılır) · **RED-2 (aynı kapının içinde):** varsayılan-rota kapısı **HAM** prefix'e bakıyordu ama normalizasyon 4-in-6'yı unmap ediyor → `::ffff:0.0.0.0/96` kapıdan `/96` geçip çözücüde **`0.0.0.0/0`** oluyordu; prod'da **sessizce**, her çağıran kendi adresini seçebiliyordu → **ikinci temsil silindi** (config v4-mapped yazımı reddediyor, httpx düşürüyor) — iki kanonikleştirme hatanın **kaynağıydı**, ve `config→httpx` import döngüsü yüzünden normalizasyon config'e taşınamıyor · **kart iki yerde düzeltildi:** klasik tenant middleware'i tap yolunda **kurulamaz** (tenant çözümlemenin **ÇIKTISI**, ADR 0002 md.7; girdi alan middleware çağıranın kendi tenant'ını adlandırmasına izin verirdi) → `httpx.Identify` **yalnız gerçekleri** taşıyor, **sıfır değeri `SessionUnresolved`** (M5-01 kutup dersi; middleware'i unutan rota **gerçek oturumsuz tap gibi görünemez**), `BySession` o durumda **500** ama `SessionAbsent` **geçiyor** (§5 satır 3 meşru); 429'da `audit_log` **yalnız kimlik sonrası** mümkün (`tenant_id` NOT NULL + FK, **uydurma tenant YOK**) · **devralınan yükümlülük kapandı:** `handler.clientIP` artık `httpx` üstünden → proxy arkasındaki çağıranlar **ayrı kova** (negatif kontrol: çözücüsüz = M5-02 hâli → 429); `floodLimit` **600'de kaldı** (düzeltilmesi gereken sayı değil **anahtardı**) · **`tapSessionLimit` ilk taslakta 120'ydi, yapıcının KENDİ testi çürüttü** (5 sn'lik yenileme döngüsü tam 120) → 300 · **TapLimiter monte EDİLMEDİ** (`/t`, `/api/checkin` yok) · **N5 yalnız oturum yarısı** — `sys:tenant-mismatch` hâlâ ölü, "kapandı" **denmiyor** · 393 test 0 SKIP, httpx %95.4 |
| M5-04 | GET /t: tap sayfası | **done** | `cfa6cd5` · **iki denetçi**, üçüncü göz **RED** · **§4.4 yeni giriş noktası:** kart "sayfa açılışında ilerletme" istiyordu ama `sun.Verify` 6. adımda ilerletiyor → `PreviewWithoutReplayProtection`. **Caydırıcı yapısal:** `Preview` ≠ `Result` (atanamaz → `Verify`-şekilli kod **derlenmez**), `SUNValid` alanı **yok**, ve denetimden sonra `db.ResolvedTag` da **taşınmıyor** (`pv.CMACValid && p.Ctr > pv.Tag.LastCtr` yazılabilir bir cümleydi = §4.4'ün yasakladığı TOCTOU; ayrıca KEK-sarmalı anahtar handler'a gidiyordu). Kalan 4 alan. **İki denetçi de beş caydırıcıyı kendi paketinden derleyerek denedi → hepsi derleme hatası** · **🔬 güvenlik denetçisi bağımsız RFC 4493 + NXP SDM türetmesi yazıp GERÇEK geçerli SUN URL'i mintledi:** 30 açılış → `last_ctr` **0→0**, aynı URL `Verify`'dan geçince **700→701 `SUNValid=true`**, replay → false. **M2-04'ün "iç-tutarlı vektör byte-sırası hatasını yakalayamaz" dersine karşı DIŞ doğrulama** → yapıcının açık bıraktığı geçerli-CMAC boşluğu **ölçümle kapandı** · **RED:** `NewTap` `TapLimiter`'ı **`Audit` olmadan** kuruyordu → **M5-03'ün ONAYLANMIŞ kriteri üretimde ölüydü** (15×429, çözülmüş `employee_id`, `audit_log` 4145→4145). **Hata kimsenin dosyasında değil, iki görevin ARASINDAydı** · **🔁 ve düzeltmenin ilk mutasyonu YEŞİL kaldı** — red testleri `tp.limiter`'ı **kendileri** kuruyordu (`Audit`'i açıkça vererek): *denetlediği şeyi kendisi kuran test hiçbir şey denetlemez*. Yapıcı kendi raporladı; testler artık **ürünün kurduğu** limiter'ı üretim bütçesiyle sürüyor. Aynı tuzak **bir alan yanında** tekrar bulundu (`Refused: nil` de yeşil kalıyordu) → o da kapatıldı · **imzalı bağlam:** çipin MAC'i sunucuda **bir kez** kontrol ediliyor, sayfaya **tek bit** geçiyor; türetilmiş anahtar + oturum id'si AAD; **dokuz sahtecilik denemesi** reddedildi, sabit zamanlı, TTL 15 dk + kayma fail-closed · **`ctr`/uid bağlamda SEYAHAT EDİYOR** (ikisi de adres çubuğunda zaten var), **CMAC etmiyor** — tersini söyleyen üç yer düzeltildi · §5 satır 3 (oturumsuz/iptal → 303, `transactions` sabit) ve satır 4 (deaktif çalışan canlı oturumla **sayfayı görüyor**, kaydı POST'a kalıyor) canlıda doğrulandı · **fontlar self-host** (6 woff2, **79.032 bayt**, latin+latin-ext Maltaca için, 2 OFL + sha256 provenance; **mutlak URL 0**) · `/static` dizin listelemesi kapatıldı, tap yanıtlarına **CSP**, `watchPosition` **0** · 933 test 0 SKIP, `internal/sun` %96.7 |
| M5-05 | POST /api/checkin: orkestrasyon | **done** | `b82c9f2` · **iki denetçi ONAY** · **M3+M4 İLK KEZ gerçek HTTP isteğiyle çalıştı**; §5'in **yedi satırı da** uçtan uca kanıtlandı (güvenlik denetçisi altısını **kendi HTTP sondasıyla** yeniden üretti) · **🔴 ölçülmüş bloklayan:** seed'li tenant'ta `policies`/`policy_versions` **0/0** → baseline katmanlı kararlar **`23503`**; guardrail satırları (1–5) yazılabiliyordu çünkü `version_id NULL` taşıyor → **delik tam olarak satır 6–7'deydi: sıradan `ok` ve `flag`, ANA YOL**. M7-03 hiç çalışmamış. Çözüm: **ilk ihtiyaçta idempotent materyalizasyon** (uuid-v5 türetilmiş id → conflict hedefi **var olan kısıt**, migration yok; `policy_versions` append-only korunuyor, `max version_no = 1`). Alternatif ("gürültülü başarısız ol") ya tap'i düşürmeye (§4.6) ya da o tenant'taki **her** tap'i review'a göndermeye indirgeniyordu · **🔴 N5 KAPANDI** — `tap.Input` iki tenant'ı taşıyor, `sys:tenant-mismatch` **ateşliyor** (403, iki tenant'ta da **0 satır**, yabancı `last_ctr` sabit, gövdede yabancı id/uid/mekân adı **yok**; kararı **guardrail** veriyor, FK değil) · **F1 (denetimden):** advance karşılaştırmadan **ÖNCE** ve **yabancı tenant'ın RLS bağlamında** koşuyordu → çapraz-tenant tap yabancı `last_ctr`'ı **900→901** yapıyor, o tenant'ta **hiç iz bırakmıyordu**; karşılaştırma öne alındı · **yapıcı state.md'nin N5 ifadesini DÜZELTTİ:** beslenmemiş hâlde *yazma* çapraz-tenant `ok` üretmezdi — `transactions_tag_fk` **23503** → 500 → **kayıt kaybı**; şema **sessiz bir ikinci ağdı** ve izolasyon ihlalini §4.6 kaybına çeviriyordu · **🔁 en değerli bulgu bir KANIT boşluğu (bu sınıf oturumda 3. kez):** üretim yazma yolundaki **gerçek TOCTOU** tüm suite'i **yeşil bıraktı** — `internal/sun` `AdvanceCounter`'ı kanıtlıyor, bu paket tap'in doğru kayıtla bittiğini kanıtlıyor, ama **çağırdığını pinleyen hiçbir şey yoktu**; mutasyon ikisinin **arasından** geçti (80 ms pencere → **12/12 SUN-valid**). Çözüm: yarışı daha çok test etmek **değil**, **çağrıyı pinlemek** (tam 1 `AdvanceTagCounter`, doğru tenant/uid/ctr, tag tenant'ının `WithTenant`'ı içinde) · **F2:** `sys:tap-freshness` **ÖLÜYDÜ** (`tap:pageAgeSeconds` beslenmiyordu); beslendi — bandı varsayılanlarla **boş** (TTL 900 == eşik 900), bu yüzden hiçbir test yakalamamıştı; M5-10 pencereyi daraltınca **erişilebilir** olacak · **F3:** N3 wiring'i **yanlışlanamazdı** (harness debounce'u varsayılana eşitti → mutasyon yeşil); harness 120 sn'ye çekildi · **F4:** dört verdict damga sınıfı **derlenen CSS'e hiç girmiyordu** (Tailwind globları **Go dosyalarını taramıyor**) → literaller `.templ`'e taşındı · **K1:** sıfır-zaman nöbetçisi `0001-01-01T00:00:00Z` ile çakışıyordu → `OccurredAt` **pointer** oldu · N1/N2 (16 sahte alan yok sayıldı)/N4 + `ErrUnknownTag` + `entered_by` + `sys:occurred-at-bound` hepsi ayrı kanıtlı · **dayanıklılık:** 9 düşmanca girdi (yıl 9999/yıl 1/±23:59 offset/±180/denormal float) → **hepsi 200 + tam bir satır, hiç 500 yok**; politika katmanı zorla düşürülünce **kayıt yine yazılıyor** (`flag`/`default`) · dokümanlar `redirect`/`ignore` beyan **edemiyor** → tenant politikası tap'i **susturamaz** · 1033 test 0 SKIP, tap %97.0 / sun %96.7 |
| M5-06 | Onay ekranı ve marka mesajları | **done** | `b3fb2b5` · **iki denetçi ONAY** (`tappa-security-auditor` + genel üçüncü göz) · **15 tur, 11 RED — projenin en uzun görevi ve neredeyse HEPSİ tek sınıftan: bir cümle ya da bir SAYI, sistemin vermediği bir şeyi beyan ediyor** · **RED-1 (§4.6, güvenlik denetçisi):** `ignored` ekranı *"Your earlier tap stands."* diyordu — debounce **verdict'ten VE kanaldan bağımsız** (`GetLastTransactionForEmployee`'de yüklem yok → `decide.go:180` koşulsuz → `guardrails.go:328` yalnız gap), yani öncül `flag`/`reject` olabilir. **Görevin `flag`'den sildiği sessiz onay kusuru yok olmamış, `ignored`'a TAŞINMIŞTI** · **RED-2:** `reject` başlığı `<h1>`'de *"Not recorded"* diyordu; `Record` INSERT'ten **sonra** hiç hata döndürmüyor (`checkin.go:569-602`) → render edilen Result **satırın kanıtı**; aynı sayfa dört satır altta "was recorded" diyordu ve yanlış cümleyi **hiçbir test yasaklamıyordu** → *"Not counted"* · **RED-3…9 hep ARACIN kendisinde:** elle kurulmuş bayt-golden üretimin hiç render etmediği bir gövdeyi pinliyordu (Note'suz **971 B** vs gerçek **1061 B**) → metin-düğümü beyaz listesi **dört kanaldan** yenildi (CSS `content`, `</main>` dışı, `aria-label`, `title`) → `<input readonly value>` (*"value machine-facing'dir"* yanlıştı) → `<iframe srcdoc>`/`<object data>`/`<img src=data:svg>` → `<link href="data:text/css,…{content:'…'}">` (izinli eleman + okunmayan öznitelik) → metin testi retry dalını **hiç render etmiyordu** + `<meta http-equiv=refresh>` → **regex öznitelik SIRASINA bağlıydı** (oturumun kanonik dersi, kendi kontrolünün içinde) · **son hâl: üç dar beyaz liste** — görünür metin (doküman + 11 öznitelik) · eleman adları (**kapalı küme 16/14, iki yönlü eşitlik**) · dış referanslar (`{/static/css/app.css}`; **markanın "mutlak URL 0" kuralı ilk kez teste bağlandı**) — artı 7 tap yanıtında pinlenen CSP · **kapatılamayan 8 kanal GARANTİ değil LİMİT olarak sayıldı** (`<meta name=description>`, navigasyon yanıt başlıkları (`Refresh:` ölçüldü), CI'da **daima SKIP** olan CSS kontrolü, runtime script, beş aktivasyon ekranında CSP yok, elle düzenlenmiş `*_templ.go`, ekran-başına elle kapsam) · **wiring boşluğu ayrıca kapatıldı:** altı hata ekranının metnini yalnız elle kurulmuş bir view pinliyordu → `renderProblem` başka şablona/view'a bağlanınca **RED 20/17** (önce ikisi de yeşildi) · **11 sonuç şekli + 6 hata ekranı ×2 + 5 DB alt testi**, beş not sabitinin **5/5**'i üretimden sürülüyor (`staleOpenInNote` 19 sa eski açık kayıt seed'iyle) · **kopya kararları §4.6'dan:** `flag` "All done" **demiyor**, onayı **vaat etmiyor**, itiraz kapısı açık · practice tap'te marka mesajı **yok** · `business_type` ile tenant mesajı (**seed UUID yok, migration yok**) · **sayı hataları tek başına bir bulgu sınıfı oldu** (`SEVEN`→8 · "üç aktivasyon ekranı"→5 · "dört dal"→5 · "iki vaka"→4 · "yedisini de"→6 · "~16:1"→**5.70:1**, çünkü kapanış cümlesi docket'in **dışında**) → alan sayısı artık **reflection teliyle** çivili · **denetçi ağaca iki kez zarar verdi** (biri `git checkout` ile commit edilmemiş 12 satırı **kalıcı** sildi, biri `basename` çakışmasıyla dosya ezdi ama `git hash-object` ile birebir kurtardı) → kural `agent-brief.md`'ye yazıldı · PASS **1158** SKIP **0**, `app.css` 14256 B, `make audit` 0 |
| M5-07 | Mini tur ve practice tap | **done** | `e0a5700` · **iki denetçi ONAY** (genel üçüncü göz 2. turda + `tappa-security-auditor`) · **görevin yarısı zaten hazırdı ve bunu ÖLÇMEK işin ilk yarısıydı:** `practice` sunucu türetimli (M4-06), TRAINING damgası + marka mesajı bastırması (M5-06) çalışıyordu · **YENİ:** `GET /activate/tour?step=1..3`, **sunucu render, JS yok, istemci state'i yok**, linklerle ilerliyor, her slayttan atlanabiliyor; `Submit` **ilk** aktivasyonu tura, **ikinci cihazı** doğrudan onaya yönlendiriyor · **tur hiçbir şey yazmıyor** (7 istek boyunca `transactions`/`audit_log` donuk + pozitif kontrol; `Set-Cookie` boş; POST/PUT/DELETE → 405) · **🔁 İKİ MASKELİ MUTANT bulundu ve kapatıldı** — `gather`'ın `if !open.Practice` guard'ı silinince **tüm suite yeşil** kalıyordu (motor tarafı `decide.go` bağımsız kanıtlı olduğu için: *bir garanti A'da kanıtlanıp B'de tüketiliyorsa B'nin onu KULLANDIĞI ayrıca pinlenmeli* — bu oturumda **üçüncü** kez), ve `transaction()`'ın `Practice: t.Practice` eşlemesi **eşdeğer mutanttı** (tek tüketicisi `resolveDirection`, filtre ayaktayken alan üretimde hiç `true` olmuyordu) → `TestGatherDB_…` + `TestTransaction_CarriesThePracticeColumn` · **§4.6 vaat analizi:** practice hakkı **herhangi bir** önceki satırla harcanıyor (`GetLastTransactionForEmployee`'de **verdict ve kanal yüklemi yok**): önceki yok → evet · `reject`/`ignored`/manuel → hayır; ilk tap **asla `ignored` olamaz**; QR ilk tap practice **olur** · **en kötü vaka ikinci cihaz** (zaten tap etmiş biri) **yapısal olarak** vaadi hiç görmüyor · güvenlik denetçisi `practice`'i **dört kanaldan** (query/header/multipart/JSON) hem iddia hem **reddetme** yönünde denedi ve sütunu geri okudu: `practice=false` gönderen ilk tap yine **`true`** · **davet kodu çerezi tura kadar yaşamıyor** (istek istek izlendi, `clear(w)` yönlendirmeden önce) · **RED (1 tur):** `assertRefs` href **DEĞER** kümesini karşılaştırıyor **sayısını değil** → slayta **metinsiz**, izinli hedefli üçüncü bir `<a>` (görünmez ikinci dokunma hedefi) eklenince suite yeşildi ve testin yorumu *"a link ADDED to a slide fails too"* diyordu → `TestTour_HasExactlyTheseTouchTargets` slayt başına **sıralı (hedef → etiket)** listesini pinliyor + `on…=` reddediyor · **`ping=` kapatıldı** (`refRE`'ye eklendi; M5-06'dan devralınan boşluk, ortak `Problem` şablonunda da RED → **on bir ekran**) · tur M5-06'nın **üç beyaz listesine de eklendi** (13 etiketlik kapalı küme) · **§4.4 kararı:** emekli plakete reject `last_ctr`'ı **ilerletmiyor** ve bu **doğru** (ilerletmek sayacı çipin önüne iter → sonraki gerçek tap'ler replay = kodun kendi adlandırdığı DoS); bedeli plaket dönünce bir kez `base:ctr-gap-review` · **kapsam dışı düzeltildi:** `internal/policy/document.go` *"EffectIgnore → no record"* **yanlıştı** (`ignored` satır **yazıyor**; mutasyonla 4 test RED) · Tailwind farkı **tek yeni seçici** `.min-h-11` (14283→14312), sıfır düzyazı-doğumlu ölü kural · **1197 test, 0 SKIP** |
| M5-08 | QR kanalı | **done** | `1d836e3` · **iki denetçi ONAY** · **8 tur, 7 RED — projenin en derin zinciri** · **başladığı yer:** QR motorda zaten bağlıydı (`Parse` kanalı üretiyor, `base:qr-requires-ip` baseline'da, `preview` anahtara dokunmadan kısa devre yapıyor); eksik olan **kanıttı** — bu pakette **hiçbir** `GET /t` isteği `&ctr=&cmac=` olmadan yapılmamıştı, yani varış yolu hiç sürülmemişti · **iki maskeli mutant öldü:** `preview.go` adım 4 silinince suite **tamamen yeşil** kalıyordu (sonuç aynı, saklanan fark: **doğrulanacak hiçbir şey taşımayan URL için AES anahtarı açılıyor**) ve `channel` sütunu hiç pinlenmemişti (`"nfc"` sabitlemesi her paketi geçiyordu) · **sonra ölçüm bir zincir açtı:** §5 satır 5 debounce'u — sayacı olmayan bir kanalın **tek freni** — **dört ayrı şekilde** aşılabiliyordu ve her biri ancak öncekini kapatınca göründü: **mesafe** (gap istemcinin `occurred_at`'inden) → **seçim** (`ORDER BY occurred_at DESC`, yani öncülü de istemci seçiyor: geçmişi olan çalışan + 20 geriye tarihli POST → **20 sayılan satır**) → **işaret** (ileri tarihli tap `sys:occurred-at-bound` ile reddedilir ama **kaydedilir**, sonra o sıralamayı kazanır, negatif gap guardrail'i tümden kapatır → sonraki 20 dürüst tap **`flag` değil `ok`**) → **eşzamanlılık** (`gather` ve `write` ayrı tx → 50 eşzamanlı POST **0,48 sn'de 51 sayılan satır**) · **iki kullanıcı kararı (2026-08-01):** iki koşullu debounce **şimdi**, ve **kişi başına advisory lock** · **bugünkü kural:** `gap = min(beyan mesafesi, DB'nin hesapladığı yaş)` — `clock_timestamp() − created_at`, yalnız tap kanalları, **manuel öncül muaf** (müdürün satırı dokunuş değil; düz `min` müdürün geçmişe tarihli girişinden 30 sn sonraki **gerçek tap'i yutuyordu**) — ve `gather`+`Decide`+`write` **tek transaction**, `pg_advisory_xact_lock(tenant‖employee)` altında · **ADR 0006** (ölçümler, **beş reddedilen alternatif**, ne garanti edilmediği) · **kilidin bedeli ölçüldü ve yazıldı:** bekleyen istek **havuz bağlantısı tutuyor** → tek anahtara flood, ilgisiz kişinin gecikmesini **6–9×** artırıyor (`pg_stat_activity`: **16 bağlantının 15'i** `wait_event='advisory'`, kontrol kolunda **0**) · **§4.6 penceresi büyüdü** (`advance` → havuz → **kilit beklemesi** → `INSERT`; dışarıdan 3 sn kilitte tap **3,32 sn**; tavan `middleware.Timeout(30s)` — küme/DB/rol'de `statement_timeout`/`lock_timeout`/`idle_in_transaction` **üçü de 0**) — güvenlik denetçisi **kabul edilebilir** buldu (şekil önceden de vardı; diff havuz alımını **3'ten 2'ye düşürüyor**; aynı kişinin kendi çakışması gerekiyor; kaybedilen kayıt zaten `ignored` olacak olan; sessiz değil) · **kapsam dışı düzeltildi:** `policy/document.go` *"EffectIgnore → kayıt yok"* **yanlıştı** (mutasyonla 4 test RED) · **7 RED'in tamamı "bir cümle, sistemin vermediği bir şeyi beyan ediyor" sınıfından**; son üçü **yalnız metin** ve aynı iddia (*"birkaç milisaniye"*) **altı kez** yeniden doğdu — bir kez aynı commit içinde bir dosyada geri çekilirken kardeşinde ayakta kaldı · **1250 test, 0 SKIP**, migration yok |
| M5-09 | Uçtan uca test ve "bir günü simüle et" | **done** | `b0044c5` · **iki denetçi ONAY** (genel üçüncü göz 2. turda + `tappa-security-auditor` koşulsuz) · **3 tur, 1 RED** · **önce bilinen engel:** seed `aes_key_ref`'e **42 baytlık düz ASCII** yazıyordu → her seed'li plaket NFC yolunda **500** (`sun.Unwrap` 44 bayt ister). Zarf **SQL literali olamaz** (operatörün KEK'ine bağlı + `Wrap` her çağrıda taze nonce çeker) → seed **iki adımlı**: `seed.sql` yüksek sesli placeholder yazar, `seed.sh` `go run ./test/fixtures/seedkeys | psql` ile **aynı role** (`tappa_owner`) zarfları basar; program **hiçbir yere bağlanmaz**, `(KEK, fixture listesi)`'nin saf fonksiyonu. Sahte anahtar `SHA-256("tappa-fake-seed-tag-key-do-not-use|"‖UID)[:16]` — repoda **ne düz ne sarmalı** değer var, yalnız tarif (§4.7) · **drift guard** (`DO $$ … RAISE $$`) **ilk hâlinde mutasyonda YEŞİL kaldı** (kapsamı `SeedTags`'ten türetiyordu, listeden plaket silinince guard da bakmayı bırakıyordu) → tenant çifti sabitlendi, mutasyon **RED** · **gün sıkıştırılamaz:** ADR 0006 debounce'u **sunucu saatiyle** ölçüyor → beklemesiz koşuda **15 kaydın 10'u** `sys:person-debounce` (tam günde 31'in 19'u), ve §4.6 gereği **15/15 satır yazılıyor**. **Motor fixture'a eğilmedi**, gün gerçek zamanda bekliyor (`policy.DebounceMinSeconds=30`; 30 sn altı `config.Load`'un reddettiği **dejenere değer** olurdu) · **RED (1. tur, iki madde, ikisi de belgeleme):** `day_db_test.go`'nun *"see the limits at the end"* atfının işaret ettiği bölüm **yoktu**, ve F5 (aşağı) hiçbir kalıcı yere yazılmamıştı → `LIMITS L1–L4` bölümü + kart md. 6 · **🔴 F5 — sevk edilmiş kodda §5 ihlali bulundu (yapıcının kendi bulgusu, iki denetçi büyüttü):** bir `practice` satırı **daha eski ve açık** bir gerçek girişi maskeliyor (`GetLastOpenTransaction` tek satır döndürüyor, tüketici practice ise **atıyor ve altına bakmıyor**) → çıkış `in` oluyor, giriş **hiç kapanmıyor**, **hiçbir sinyal yok**. **Düz HTTP'den erişilebilir** (`occurred_at` sevk edilmiş alan, tavan 72 sa). Denetçi bunu **günün kendi testiyle** üretti (workaround kaldırılınca `day_db_test.go:546` RED) → **kullanıcı kararı 2026-08-02: kendi görevinde düzeltilecek → M5-11** · **`assertAfterShiftStart` YAPISAL OLARAK BOŞTU** (mutasyon: `lateness → return nil` ile gün o satırdan sorunsuz geçti) → kaldırıldı, "geç kalma" senaryosu **HTTP'de üretilmiyor** diye dürüstçe sayıldı · **`assertTellableApart` dejenere değer tuzağına düştü** (beklenen de fiili de aynı `f.runStamp`'tan; `newRunStamp → "CONSTANT"` süiti **yeşil** bıraktı — *kendi dosyasının adını koyduğu tuzak, onu önlemek için yazılmış kontrolün içinde*) → saf `TestRunStampVariesBetweenRuns` + satır-sayısı kolu, mutasyon **0,96 sn'de RED** · **senaryo tablosu dürüstçe sayıldı: 12'den 10'u HTTP üzerinden, 1'i HTTP dışı** (`manual` — uç nokta **uydurulmadı**, M6-04'ün; router'ın kullandığı **aynı `checkin.Service`**), **1'i hiç üretilmiyor** (geç kalma) · **NFC'de tek stipülasyon:** sayfa gerçekten açılıyor (gerçek `Parse`+unwrap+preview) ama CMAC biti çevriliyor — geçerli CMAC üretmek SDM'nin **ikinci uygulaması** demekti (repo bunu iki kez reddetti) → **LİMİT olarak sayıldı, kapatılmadı** · **kullanıcı kararı 2026-08-02 (süre):** `make test` **tam kalsın** (98,5 sn; CI değişmedi) + **`make test-short`** (32,9 sn, **tam 1 SKIP**, yüksek sesli mesaj) — `t.Parallel()` **elendi** (3/3 kırmızı: testler **aynı seed'li plaketin `last_ctr`'ını** paylaşıyor, paralelde tap replay sayılıyor ve biri **§4.4 kırmızı-çizgi testini olmayan bir ihlalle** kızarttı) · **`make audit` bu görevde ZAYIF KANITTI:** `redline-check.sh` `SRC`'si `test/`'i **içermiyordu** → yeni dört dosyanın **ikisi** hiç taranmıyordu; güvenlik denetçisi dokuz deseni de elle koşturdu (hepsi temiz) ve `SRC`'ye `test` eklendi (**sıfır yanlış pozitif**, ölçüldü) · **1255 test, 0 SKIP** · migration **yok** |
| M5-10 | Tap tazelik penceresi (URL biriktirmeye karşı) | **done** | `68acb81` · **iki denetçi ONAY** · **6 tur, 4 RED** · **kartın yarısı gerçek dışıydı ve bunu ÖLÇMEK işin ilk adımıydı:** kart M5-04'ten **önce** yazılmış, oysa M5-04 imzalı tap bağlamını getirdi — `IssuedAt` **sunucu saati**, payload'ın 8. alanı, **MAC'in içinde**, `tap:pageAgeSeconds` → `sys:tap-freshness` zinciri **çalışıyor**. Eksik olan tek şey iki sayının eşitliğiydi (eşik 900 == `tapContextTTL` 900 → guardrail **hiç ateşlenemiyordu**). **Kullanıcı kararı 2026-08-02: `tap_page_views` tablosu YAPILMADI** — tablo **koruma tarafında sıfır** ekliyor (pencere `GET` anından ölçülüyor ve o anı **saldırgan seçiyor**), tek gerçek katkısı M6-11'in *"POST'suz GET"* metriğiydi → o karta devredildi. **Migration YOK.** Üretim: **13 eklenen yorum-dışı satır** (`config.go` 6 + `checkin.go` 7) **+ 18 yer değiştiren** (`guardrails.go`, net yeni mantık **sıfır** — asıl güvenlik değişikliği o blok taşımasıdır) · **kullanıcı kararı 2026-08-02 (§4.6 eşiği):** `tapContextTTL` **15 dk kaldı** → `<180` normal · `180–900` **kayıtlı** reject · `>900` **kayıtsız 400**, LİMİT olarak sayıldı · **🔴 asıl bulgu genel gözün DEĞİL, `tappa-security-auditor`'ın:** bandı açmak bir **regresyon** üretti — `sys:tap-freshness` sırada **#4**, `sys:employee-deactivated` **#7** → deaktif oturum **3 dakika bekleyerek** güvenlik uyarısını düşürüyordu (§5 satır 4 ihlali; ölçüm `window=15m → ALERTS +1`, `window=3m → ALERTS +0`). **Aile taraması ikinci ve DAHA ESKİ bir örnek buldu:** `sys:occurred-at-bound` de düşürüyordu ve tetiği **daha ucuz** (bekleme değil, bir POST **form alanı**) · **çözüm tek slice hamlesi:** §5'in adlandırdığı beş satır artık §5'in kendi sırasında, adlandırmadığı iki zamanlama kuralı arkada; `sun-invalid`'in ön-alması **kasten korundu** (sahte tap uyarı imal etmemeli, R8) · **ADR 0007** (ölçüm · aile tablosu · iki reddedilen alternatif **ölçümleriyle** · **beş garanti-dışı**) · **🔁 eski yerleştirme kuralı REGRESYONU KABUL EDİYORDU** (*"sırayı bozmayan her yere konabilir"* — ölçüldü: hatalı sıra da bu kuralı sağlıyordu) → yerine **yapıyı** kontrol eden invaryant: *uyarı taşıyan bir guardrail'in önündeki her şey **adlandırılmış istisna** olmalı* · **ve sabit listeli test bir AĞ DEĞİL, DEĞİŞİKLİK DEDEKTÖRÜDÜR:** yeniden sıralamayı yakalıyor ama **eklemeye karşı çaresiz** (kırmızı testin doğal onarımı listeyi güncellemektir = tam olarak yanlış hamle); denetçi bunu sahte bir 11. guardrail'le kanıtladı — **üç paket de yeşil** kalıyordu · **çapraz çarpım ölçüldü** (864 kombinasyon, 78 kazanan değişti; **uyarı kaybeden 0**, kayıt kaybeden 52'nin **hepsi** oturumsuz = §5 satır 3'e uygun **ve üretimde erişilemez**, üç bağımsız kanıtla) · kapsam dışı iki yanlış cümle düzeltildi (sahte SUN + lost tag **uyarı imal edebiliyor**; `+nan` ParseFloat'ta geçersiz) · **1289 test, 0 SKIP** |
| M5-11 | Practice satırı açık girişi maskeliyor (§5 yön ihlali) | **done** | `1a945fd` · **iki denetçi ONAY** · **2 tur** · **sevk edilmiş kodda bir §5 ihlalinin düzeltmesi**, M5-09'da bulunmuş, kullanıcı kararıyla kendi görevi olmuştu · **kusurun adı bir CÜMLEYDİ:** `decide.go` *"primary enforcement is the caller's query, **which excludes practice**"* diyordu — **sorgu dışlamıyordu**; dışlamayı tüketici yapıyordu ve yalnız **dönen tek satır** için · **düzeltme TEK SATIR** (`AND NOT t.practice`, kaynak + üretilen kodda; yorum-dışı diff **1**), şema değişmedi, migration yok · **kartımdaki senaryo YANLIŞTI ve yapıcı ölçümle çürüttü:** *"yeniden aktivasyon ikinci practice verir"* — vermiyor (`isPracticeTap` `LastForPerson == nil` istiyor **ve** `ConsumeInviteAndActivate` `activated_at`'i `COALESCE`'luyor; ölçüldü: before == after). Gerçek erişilebilir yol **geriye tarihli `occurred_at`** (72 sa, ADR 0004 §11) — yani **M9-01 kuyruğunun ürettiği şekil** · **`NOT EXISTS` practice-nötr KALABİLDİ** çünkü *"practice ⟹ `in`"* bir **invaryant**: `TestDecide_PracticeIsAlwaysAnIn` bir **özellik** testi (72 kombinasyon + iki boş-olmama sayacı), liste değil · **bitiş şartı yerine geldi:** gün testindeki workaround (`declaring(rbGPS, night.practice)`) **silindi**, `nightTimes.practice` alanı da kaldırıldı, gece vardiyası **fixture yardımı olmadan** geçiyor (`make simulate-day` PASS ~64 sn); düzeltme geri alınınca **üç bağımsız yerde** kırmızı · `LIMITS L3`, M5-09 kart md. 6 **ve** M4-04'ün *"onaylanmış ama fiilen sağlanmayan"* kriteri kapatıldı · **ADR 0008** · **🔴 güvenlik denetçisinin şartı — ADR KENDİ DÜZELTTİĞİ HATAYI TEKRARLIYORDU:** *"düzeltme yolu mekanizma olarak ZATEN VAR … + `audit_log`"* yazıyordu; ölçüldü: **408 manuel satır / 0 `audit_log` satırı / 0 HTTP rotası**. Düzeltildi — şekil §4.3'ün emri, domain yolu **var** (`ErrEnteredByRequired`), `audit_log` ve müdür yüzeyi **M6-04'ün ve bugün YOK** · **maliyet ölçüldü ve DoS değil:** yüklem indeksi kullanmıyor (`Filter`, `practice` indekste yok) ama sıradan şekilde **bedava** (11 300 → 11 300 buffer) ve practice-en-üstte şeklinde bile sıradan şeklin **altında** kalıyor (50k satırda 123–164 ms vs 150–190 ms); `Timeout(30s)`'e ulaşmak kişi başına **~3 milyon satır** = iki aylık kesintisiz flood, ve zarar **saldırganın kendisiyle** sınırlı (kişi-bazlı advisory kilit) · **1299 test, 0 SKIP** |

### M6 — [Admin dashboard](m6-dashboard.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M6-01 | Admin kimlik doğrulama | **done** | **B fazı `4bc2e72`** · **iki denetçi ONAY** · **18 tur, 8 RED — projenin en uzun görevi** · bcrypt (Q03, `golang.org/x/crypto` eklendi, `make audit` yeşil) · admin oturumu · giriş + seçici ekranları · oran sınırı · `audit_log` · **yeni migration YOK** · **beş yükümlülüğün beşi karşılandı ya da limit yazıldı** · 🔴 **beş koruma sevk edildi ve BEŞİNİN DE silinmesi suite'i yeşil bıraktı** (`isLookupableEmail` · `sessionGate` · `sameOriginGate` · `meterOnly`'nin ücretlendirmesi · `CookiePath`/`maxCandidates`'in **totolojik** testleri) — her biri ayrı turda, ayrı denetçi, **mutasyonla** · 🔴 **53× zamanlama kehaneti** (>72 baytlık parola bcrypt'i kısa devre yaptırıyordu; kayıtlı e-posta **5,53 ms**, kayıtsız **295,42 ms**, tek istekle kesin, **sunucuya maliyeti sıfır bcrypt**) — güvenlik merceği genel gözün ONAY'ından **sonra** buldu · ve düzeltmesi bir sonraki turda yakalandı (üçüncü `-short` skip'i kehanetin **iç döngüdeki tek savunmasını** sildi) · **`GET /admin` çerezsiz çağırana bütçesiz `SECURITY DEFINER` okuması ödetiyordu** (uydurma token 1,36 ms vs bozuk 156 µs, 600 istekte 0×429) · onu kapatan flood kapısı **çıkışı reddedip oturumu canlı bırakıyordu** (orkestratörün kararının regresyonu: `tap.go`'nun **ByAddress → Identify → BySession** deseninin yalnız ilk aşaması uygulanmıştı) · **sayı hijyeni altı kez bulgu oldu** (`test-short` bandı üç kez dar yazılıp üç kez tutmadı → format **gözlenen aralığa** çevrildi; 00011'in `cost-10` sayısı **~4× iyimser**, sevk edilen digest'ler cost 12) · **1633 test, 0 SKIP** · **12 limit yazılı** · *(A fazı `66d5442`, 3 tur — aşağıda)* **A fazı `66d5442`** · **iki denetçi ONAY** (genel üçüncü göz 2. turda + `tappa-security-auditor` koşullu, koşullar kapatıldı) · **3 tur** · M5-02'nin A/B kalıbı: **veri katmanı** önce, auth+ekran sonra · **🔴 kart bir şeyi söylemiyordu ve şema onu VARSAYIYORDU:** 00006 *"resolver YOK: giriş tenant'ı biliyor"* diyor ama **hiçbir şey tenant'ı kurmuyordu** (e-posta yalnız `(tenant_id, email)` içinde tekil, slug yok, tek `/api/auth/login`) → **kullanıcı kararı 2026-08-02: global çözümleme + tenant seçici** (kullanıcının kendi demo tenant'ları KF+KM **aynı kişiye ait**) · **migration 00011:** iki SECURITY DEFINER fonksiyon (ADR 0002 md.7 kalıbı; resolver sayısı **3→5**, `tappa_resolver` sütun-SELECT'i **5 tabloda 26 sütun**, tablo-düzeyi yetkisi **sıfır**) · **`resolve_admin_by_email` beş kısıttan birini BİLEREK kırıyor:** dönüş **≤1 değil N satır**, sınır kısmi unique indeksten geliyor ve **saldırgan tarafından büyütülebilir** (M7-02 kayıt açılınca) — yazıldı · **şema sertleştirmesi repoda HİÇ ADI GEÇMEMİŞ iki yeteneği kapattı:** `admin_sessions.admin_user_id`'yi yeniden yönlendirme (**yetki yükseltme**) ve `token_hash`'i ezme (**oturum ele geçirme**); sütun-kapsamlı UPDATE ikisini kapatıyor ama **un-revoke'u kapatamıyor** (*"grant hangi SÜTUN der, hangi DEĞER demez"*) → **monotonluk trigger'ı**, `tappa_owner`'ı da bağlıyor · **🔬 en ince bulgu:** `citext`'in `=` operatörü `public`'te; `search_path=pg_catalog,pg_temp` altında **görünmez** ve Postgres **hata vermeden** `text=text`'e düşüyor → kimlik doğrulama araması sessizce **harfe duyarlı** oluyor (ölçüldü: küçük/büyük harf **0 satır**). Düzeltme `OPERATOR(public.=)` + kalıcı negatif kontrol. Tuzak **`search_path` özelliğidir**, SECURITY DEFINER'a özgü değil; şemadaki diğer citext sütunu `employees.email` bugün hiçbir sorguda filtrelenmiyor → sınıf kapalı ama **kapalılık yazıldı** · **§4.7: hash artık çıplak `string` DEĞİL** — altı basma yolu (`%+v`, dilim `%v`, `%#v`, `fmt.Errorf`, unexported alan, `slog`) hash'i **verbatim** sızdırıyordu; repo'nun kendi kalıbı (`session.Token`/`invite.Code`) **üçüncü kez** uygulandı, ve **pozitif kontrol testin körü olmadığını kanıtlıyor** · `redline-check.sh` R7 desenine **`password` eklendi** (ölçüldü: 0 yanlış pozitif) — **ve yakalamadığı dürüstçe yazıldı** (altı yolun hiçbirinde `password` **kelimesi** log çağrısında geçmiyor) · **B fazına BEŞ yükümlülük**, dört yerde: numaralandırma · kukla bcrypt · oran sınırı · **bcrypt amplifikasyonu** (bir e-posta 500 tenant'ta → 500 satır, DB **0,9 ms**, ama B fazı 500 bcrypt = **~30–50 sn CPU**, **~500×**, tek kimliksiz istekten) · **🔴 aday↔parola bağı** (`tappa-security-auditor`'ın bulduğu **en ağır** madde: oturum **yalnızca hash'i eşleşen adaya** verilmeli, seçici **yalnızca eşleşenleri** göstermeli — yoksa saldırgan kurbanın e-postasını kendi tenant'ına yazıp **kendi satırında** doğrulanır ve **kurbanın işletmesini seçer**; §4.5 çapraz-tenant kimlik atlatması, canlı ölçüldü) ⚠️ ve **4. ile 5. madde birbirinin tersine çekiyor** — *"ilk eşleşmede dur"* DoS'u azaltır ama seçici tüm adayları gösterirse **tam olarak bu atlatmadır**; gerilim dört yerde de yazılı · güvenlik denetçisi **on beş** saldırı denedi (`ON CONFLICT DO UPDATE`, `MERGE`, `session_replication_role`, `pg_temp` operatör/tablo enjeksiyonu, çapraz-tenant forge…), **on beşi de bloklandı** · down/up **tam tersinir** · **1331 test, 0 SKIP** |
| M6-02 | Dashboard iskeleti ve docket bileşenleri | **done** | **`6757537`** · üçüncü göz **ONAY** · **10 tur, 5 RED** · `layout.Panel` + `TabBar` + `EmptyState` + üç CSS ailesi; **beş sekme rotası tek tablodan** `Protect()` içinde mount ediliyor (nav da aynı tablodan → *"linki 404 veren sekme"* **üretilemez**) · **kartı ölçmek iki kez kendini ödedi:** docket motifi + beş damga **zaten sevk edilmişti** (**M0 iskeleti** `7e12f37`, M5-06 değil — M5-06 yalnız **anatomiyi** değiştirmiş; perforasyon görseli **hiç var olmamış**) → üç kriter **karşılanmıştı**, iş eksik **dört bileşendi**; ve **M6-01'in bütçe borcu ölçümle kapandı** (bir sekme görüntülemesi = **1 ücretli istek**, `/static` kapı dışında → pay **11,5×**, üç sabit **değişmedi**) · 🔴 **sevk edilmiş kontrast hatası bulundu ve düzeltildi:** `.docket-label` **3,13:1** (AA 4,5:1) **12 çağrı yerinde** — **tap ve onay ekranları dahil** — artı beş ton daha **2,40–4,36:1** → **kullanıcı kararı: hepsi `ink/70`** (en kötü zemin **5,58:1**); `/60` ölçülüp **reddedildi**; wordmark **WCAG 1.4.3 logotype istisnasını REDDETTİ**, gerekçe yazılı · **ürünün İLK kontrast testi** geldi ve üç `TestCompiledCSS_*`'in aksine **CI'da koşuyor** (paleti config'den türetir, WCAG'i yeniden hesaplar, **sıfır çağrı yerinde koşmayı reddeder**, **bağlayıcı zemini pinler** — işaret silinirse de kırmızı) · ⚠️ **bağlayıcı zemin porcelain değil `green-lite`** (L 0,8229 < 0,8627); iki tur **ve** orkestratörün brief'i yanlış varsaymıştı, **türetilmiş test yakaladı** · **filtre çubuğu ve HTMX M6-03'e TAŞINDI** (kullanıcı kararı, iki kartta da yazılı) · **1647 test, 0 SKIP** |
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

**Özet:** 83 görev · done **54** · **wip 0** · blocked 0 · skipped 1 · todo **28** · **M0+M1+M2+M3+M4+M5 TAMAM 🎉 · M6 2/12** *(M5-11 M5-09'da bulunan §5 ihlali için kullanıcı kararıyla açıldı → toplam 82'den 83'e)*

---

## Oturum günlüğü

En üste ekle. Kısa tut: ne yapıldı, ne öğrenildi, ne kaldı.

### 2026-08-03 (5. oturum, devam) — **M6-01 KAPANDI (A+B)** · panel girişi · **18 tur, 8 RED — projenin en uzun görevi**

**Ne yapıldı.** M5-09/M5-10/M5-11 ile M5 kapandı (11/11; ayrıntı dosyanın başındaki bloklarda), sonra
M6-01 **A/B kalıbıyla** bölündü ve ikisi de sevk edildi: A (`66d5442`) global admin çözümlemesi + şema
sertleştirmesi, B (`4bc2e72`) bcrypt + oturum + iki ekran + oran sınırı + `audit_log`. 00011'in **beş
yükümlülüğünün beşi** karşılandı ya da limit yazıldı. **12 limit** devredildi.

**Ne öğrenildi — bu oturumun tek büyük dersi.** *Bir düzeltmenin kendi ağı AYNI TURDA ölçülmezse, düzeltme
yazılmamış sayılır.* Beş koruma sevk edildi ve **beşinin de silinmesi suite'i yeşil bıraktı**; hepsi ayrı
turda, ayrı denetçi tarafından, **mutasyonla** bulundu. İkinci ders: **iki mercek birbirinin yerine
geçmiyor** — genel üçüncü göz 5. turda ONAY verdi, `tappa-security-auditor` hemen ardından **sömürülebilir
bir 53× zamanlama kehaneti** buldu. Üçüncüsü: **sayı hijyeni kendi başına bir bulgu sınıfı** (altı vaka);
dar bir bant üç kez yazılıp üç kez tutmadı, format **gözlenen aralığa** çevrilince bitti.

**Orkestratör kendi hatasını da kaydediyor:** 12. turda denetçinin sunduğu iki seçenekten *"bütçeyi ekle"*yi
seçtim ve gerekçem reponun kendi sırasıydı — ama `tap.go`'nun **ByAddress → Identify → BySession** deseninin
yalnız **ilk aşamasını** uygulattım. Sonuç 13. turda ölçüldü: flood kapısı **çıkışı reddedip oturumu canlı
bırakıyordu**, ve sunucu tarafı süre olmadığı için o pencerede oturumu bitirecek hiçbir yol yoktu. *Bir
deseni kopyalarken kaç parçası olduğunu saymak, hangi parçayı kopyaladığını bilmekten daha önemli.*

**Ne kaldı.** M6-02 (docket iskeleti) — ve **üç sayıyı devralıyor**: `adminSessionLimit` (kopyalandı,
türetilmedi, iki tavandan **dar** olanı), `adminFloodLimit` (kimliği doğrulanmış yüklemeleri de taşıyor),
`sessionGate`'in sınırladığı iş (bugün **boş**, M6-02 dolduruyor).

### 2026-08-01 (5. oturum, devam) — **M5-08 done** · QR kanalı · **debounce dört katmanda sertleştirildi**

`1d836e3`. İki denetçi ONAY, **8 tur, 7 RED**. Görev QR'ı **kanıtlamakla** başladı (motor zaten bağlıydı),
ölçüm bir zincir açtı ve **karar motoru değişti**.

**Zincir, ve her halkası ancak öncekini kapatınca göründü.** §5 satır 5 debounce'u — sayacı olmayan bir
kanalın **tek freni** — dört şekilde aşılabiliyordu: **mesafe** (gap istemcinin `occurred_at`'inden) →
**seçim** (`ORDER BY occurred_at DESC`, yani öncülü de istemci seçiyor) → **işaret** (ileri tarihli tap
reddedilir ama **kaydedilir**, sıralamayı kazanır, negatif gap guardrail'i **tümden kapatır** → sonraki
dürüst tap'ler `flag` değil **`ok`**) → **eşzamanlılık** (50 eşzamanlı POST, 0,48 sn, **51 sayılan satır**).
Kullanıcı iki kez *"şimdi düzelt"* dedi: **iki koşullu debounce** ve **kişi başına advisory lock**. ADR 0006.

**Bir düzeltmenin kendisi meşru bir akışı kırabilir.** Düz `min` kuralı, müdürün geçmişe tarihli girişinden
30 sn sonra gelen **çalışanın gerçek tap'ini** yutuyordu → `created_at` bacağı yalnız tap kanallarında.
Ve çevrimdışı kuyruğu (M9-01) gerçekten kırıyor: **ifşa edildi, gerekçelendirilmedi**.

**🔬 İki YÖNTEM dersi, ikisi de bir denetçinin/yapıcının ölçümünü çürüttü:**
1. **Genel denetçinin "kilit masumdur" kontrol grubu artefakttı** — flood'u **tek oturumdan** sürdüğü için
   istekler `BySession` 300/10dk'ya takılıp kilide **hiç dokunmadan** 429 alıyordu (200 istek 40 ms'de bitti).
   Temiz A/B (flood **ayrı oturumlardan**, kurban **tek atış**): ilgisiz kişinin gecikmesi **6–9×**, ve
   `pg_stat_activity` doğrudan gösterdi: **16 bağlantının 15'i** `wait_event='advisory'`, kontrol kolunda **0**.
2. **Yapıcının "0 hata" yarış ölçümü** konfigürasyon başına **tek örnek** aldığı ve sondanın **id-verme
   şekli üretimden farklı** olduğu için yanlıştı. Doğruyu ancak **gerçek kod yolunu** sürerek buldu.
   *(Ben denetçinin "harness ROLLBACK ediyor" hipotezini olduğu gibi aktarmıştım; yapıcı `WithTenant`'ın
   commit ettiğini gösterip **ölçümle itiraz etti** ve haklıydı.)*

**Ve kalıcı ders:** 7 RED'in tamamı *"bir cümle, sistemin vermediği bir şeyi beyan ediyor"* sınıfından;
son üçü **yalnız metin**. Aynı iddia (*"birkaç milisaniye"*) **altı kez** yeniden doğdu — bir kez **aynı
commit içinde** bir dosyada geri çekilirken kardeşinde ayakta kaldı. Yapıcı bir turda bunu düzelttiğini
raporlayıp düzeltmediğini de **kendi buldu** (betiği assert'e takılmış, grep'i geri-çekme metnini eliyordu).

**Sırada:** M5-09. ⚠️ **Önce seed'in `aes_key_ref`'ini KEK-sarmalı yap** — yoksa NFC yolu 500 verir ve
"bir günü simüle et" çalışmaz (QR yarısı bugün çalışıyor). Ve **`make db-reset`**: benchmark 20 002 satır bıraktı.

### 2026-08-01 (5. oturum, devam) — **M5-07 done** · aktivasyondan onay ekranına akış tamam

`e0a5700`. İki denetçi ONAY, **2 tur**. Yeni olan: `GET /activate/tour?step=1..3` — üç slayt,
**sunucu render, JS yok, istemci state'i yok**, her slayttan atlanabiliyor, ve **hiçbir şey yazmıyor**
(7 istek boyunca satır sayıları donuk + pozitif kontrol). `Submit` ilk aktivasyonu tura, **ikinci
cihazı** doğrudan onaya gönderiyor — çünkü zaten tap etmiş birine *"ilk tap'in deneme"* demek gerçek
bir check-in'i deneme sandırırdı.

**Görevin yarısı zaten hazırdı ve asıl iş bunu ÖLÇMEKTİ.** Ölçüm iki **maskeli mutant** buldu:
`gather`'ın `if !open.Practice` guard'ı silinince **tüm suite yeşil** kalıyordu (motor tarafı
`decide.go`'da bağımsız kanıtlı olduğu için), ve `transaction()`'ın `Practice` eşlemesi **eşdeğer
mutanttı**. **Oturumda üçüncü kez aynı şekil:** bir garanti A paketinde kanıtlanıp B'de tüketiliyorsa,
**B'nin onu kullandığı ayrıca pinlenmeli**.

**Tek RED de aynı aileden ve öğretici:** bir test yorumu *"a link ADDED to a slide fails too"* diyordu;
`assertRefs` href **DEĞER** kümesini karşılaştırıyor, **sayısını değil** → slayta **metinsiz**, hedefi
zaten izinli üçüncü bir `<a>` eklenebiliyordu: görünmez ikinci bir dokunma hedefi, tam da §9'un baktığı
şey. Düzeltme slayt başına **sıralı (hedef → etiket)** listesini pinliyor.

**Vaat ölçüme indirildi (§4.6).** Practice hakkı **herhangi bir** önceki satırla harcanıyor
(`GetLastTransactionForEmployee`'de verdict ve kanal yüklemi yok), o yüzden slayt *"your **first** tap"*
diyor ve *"Whatever a tap turns out to be, the screen right after it says so"* ile kapanıyor. Güvenlik
denetçisi `practice`'i **dört kanaldan** (query · header · multipart · JSON) hem iddia hem **reddetme**
yönünde denedi ve sütunu geri okudu: `practice=false` gönderen ilk tap yine **`true`**.

**Ve ileriye dönük iki kart düzeltildi** — bu oturumun kalıcı dersi: *bir görevi kapatan değişiklik,
o davranışı tarif eden **ileri** kartları da kapatmak zorunda.* M6-07 ve M6-11'in "çıkışsız açık kayıt"
kriterleri practice istisnasını saymıyordu → M6'da **her yeni çalışanın deneme tap'i** müdürün "eylem
gerekiyor" kuyruğunda belirecekti. (Aynı sınıf bir tur önce `m6-dashboard.md:56`'da yaşandı ve bu
oturumda `state.md`'de **iki kez** benim hatam olarak çıktı.)

**Sırada:** M5-08 (QR kanalı). ⚠️ Ana tuzağı yazılı: **QR'da ilerletilecek sayaç yok**, tek savunma
60 sn person-scoped debounce.

### 2026-08-01 (5. oturum, devam) — **M5-06 done** · onay ekranı bitti · **15 tur, 11 RED**

`b3fb2b5`. İki denetçi ONAY (`tappa-security-auditor` + genel üçüncü göz). **Projenin en uzun görevi**, ve
sebebi öğretici: iş bittikten sonra **on bir turun tamamı korumanın kendisiyle** geçti.

**İlk iki RED ekranın metnindeydi ve ikisi de §4.6.** `ignored` ekranı *"Your earlier tap stands."* diyordu;
debounce **verdict'ten ve kanaldan bağımsız** çalıştığı için öncül bir `flag` (onay kuyruğunda, saate
girmemiş) olabilir → görevin `flag`'den sildiği sessiz onay kusuru **yok olmamış, `ignored`'a taşınmıştı**.
`reject` başlığı sayfanın **en büyük yazısında** *"Not recorded"* diyordu; oysa `Record` INSERT'ten sonra
hiç hata döndürmüyor, yani render edilen bir Result sayfası **satırın kanıtı** — ve aynı sayfa dört satır
altta "was recorded" diyordu. **Yanlış cümleyi hiçbir test yasaklamıyordu.**

**Kalan dokuz RED'in hepsi DÜZELTMENİN içinden çıktı.** Her koruma bir sonraki turda yenildi: elle kurulmuş
bayt-golden üretimin **hiç render etmediği** bir gövdeyi pinliyordu (Note'suz 971 B vs gerçek 1061 B) →
metin-düğümü listesi CSS `content`, `</main>` dışı, `aria-label` ve `title` ile yenildi → `<input readonly
value>` (*"value machine-facing'dir"* gerekçesi yanlıştı) → `<iframe srcdoc>`/`<object data>`/`<img
src=data:svg>` → `<link href="data:text/css,…{content:'…'}">` (izinli eleman, okunmayan öznitelik) →
metin testi retry dalını **hiç render etmiyordu** → `<meta http-equiv=refresh>` → ve o kontrolün regex'i
**öznitelik sırasına bağlıydı**, yani oturumun kanonik dersi (*kontrol ile tüketici aynı temsili görmeli*)
**kendisini enforce etmek için yazılmış kontrolün içinde** tekrarlandı.

**Kırılma noktası 11. turdaydı ve bir mekanizma değil bir KURAL'dı:** *yeni kanal kapatılmıyor, dürüstçe
LİMİT olarak sayılıyor; ve "tamamen/bitmiş/complete" yazmadan önce onu yenmeye çalış.* İş üç tur sonra
bitti. Bugün **8 kanal limit olarak yazılı** — kapatılamayanı saymak, kapatıldığını iddia etmekten güvenli.

**Sonuç:** üç dar beyaz liste (görünür metin + 11 öznitelik · eleman adları, **kapalı küme 16/14, iki yönlü
eşitlik** · dış referanslar, `{/static/css/app.css}`) + 7 tap yanıtında pinlenen CSP. Markanın *"mutlak URL
sayısı 0"* kuralı **ilk kez teste bağlandı**. Ayrı bir wiring boşluğu da kapandı: altı hata ekranının
metnini yalnız elle kurulmuş bir view pinliyordu → `renderProblem` saptırılınca artık **RED 20/17**.

**İki süreç dersi (ikisi de `agent-brief.md`'ye yazıldı):**
- **Denetçi mutasyonunu `git checkout` ile geri ALMAZ** — bir denetçi commit edilmemiş **12 satırı kalıcı
  sildi**; bir başkası yedeklerken `basename` çakışmasıyla dosya ezdi (`git hash-object` ile birebir
  kurtardı ve **kendisi bildirdi**).
- **Yapıcının kendi hatasını bildirmesi ve yanlış bir talimata ölçümle itiraz etmesi iki kez iş kurtardı:**
  bir yeşil kalan mutasyonu kendi buldu, ve *"büyük harfli meta `.templ`'den üretilemiyor"* diye gelen
  düzeltme talimatını `make templ` çıktısıyla çürüttü (templ büyük harfi **birebir koruyor**).

**Sırada:** M5-07 (mini tur + practice tap). `practice` bayrağı ve TRAINING damgası zaten çalışıyor;
M5-07'nin işi turun kendisi.

**Aynı gün, kapanış:** damga kontrastı kullanıcıya soruldu ve **karar alındı — *kelime `ink`, durum rengi
çerçevede*.** Eşleme korundu, palete yeni token girmedi, beş damga da AA'yı geçiyor (13.27–15.55; önce
render edildiği hâliyle **üçü** altındaydı). `opacity-80` kaldırıldı. **skill `tappa-brand` güncellendi** —
skill'in kendi *"Kontrast AA"* kuralını iki damgada çiğnediği açıkça yazıldı. Ve bu küçük iş **iki ders**
üretti: (1) **Tailwind `.templ`'i YORUMLAR DÂHİL ham metin olarak tarıyor** — düzyazıda geçen `opacity-80`
gibi bir kelime **gerçek ölü kural derliyor** (ölçüldü: iki isim = **+330 bayt**), ve tuzak **zaten
ateşlenmiş**: `app.css` bugün **yedi** ölü kural taşıyor (`.filter` 185 B · `.visible` · `.relative` ·
`.min-h-16` · `.static` · `.fixed` · `.hidden` = **334 bayt, %2.34**), hiçbiri **95 `class` özniteliğinin**
hiçbirinde yok. (2) **Bir değişiklik `state.md`'yi yanlışlayabilir:** denetçinin bloklayan bulgusu koda
değil **bu dosyaya** çıktı — kontrast satırları düzeltmeden önce hâlâ *"kullanıcı kararı bekliyor"* ve
*"1.52:1 / opacity:.8"* diyordu. **Ders: bir görevi kapatan değişiklik, o görevin devir notlarını da
kapatmak zorunda** — yoksa sonraki oturum var olmayan bir kusuru taşır.

### 2026-07-31 (5. oturum) — **M5-05 done** · 🎉 **uçtan uca check-in ÇALIŞIYOR**

`b82c9f2`. İki denetçi ONAY. **M3+M4 ilk kez gerçek bir HTTP isteğiyle çalıştı** — policy motoru,
10 guardrail ve `tap.Decide` bugüne kadar yalnız saf paket olarak, tablo testleriyle kanıtlanmıştı.
**§5'in yedi satırı da uçtan uca kanıtlandı**; güvenlik denetçisi altısını **kendi HTTP sondasıyla**
yeniden üretti.

**Ölçülmüş bloklayan — ürünün en sık yolunda saklıydı.** Seed'li tenant'ta `policies`/`policy_versions`
**0/0**'dı → baseline katmanlı kararlar `23503`. Guardrail satırları (§5 1–5) **yazılabiliyordu**,
çünkü onlar `policy_version_id NULL` taşıyor. Yani delik tam olarak **satır 6 ve 7'deydi: sıradan `ok`
ve `flag`, ana yol.** M7-03 hiç çalışmamıştı. Çözüm: **ilk ihtiyaçta idempotent materyalizasyon**
(uuid-v5 türetilmiş id → conflict hedefi **var olan kısıt**, migration yok; append-only korunuyor).
"Gürültülü başarısız ol" alternatifi ya tap'i düşürmeye (§4.6) ya da o tenant'taki **her** tap'i
review'a göndermeye indirgeniyordu — ikisi de ölçümle gösterildi.

**🔴 N5 KAPANDI.** `tap.Input` iki tenant'ı taşıyor, `sys:tenant-mismatch` ateşliyor: **403**, iki
tenant'ta da **0 satır**, gövdede yabancı id/uid/mekân adı yok (denetçi yabancı mekâna "SECRET RIVAL
VENUE" adını verip sıfır sızıntı ölçtü), ve kararı **guardrail** veriyor — FK değil.
İki denetim deliğin kendisinden fazlasını buldu:
- **F1:** `advance`, karşılaştırmadan **ÖNCE** ve **yabancı tenant'ın RLS bağlamında** koşuyordu →
  çapraz-tenant tap yabancı `last_ctr`'ı **900→901** yapıyor, o tenant'ta **hiç iz bırakmıyordu**.
- **Yapıcı bizim N5 ifademizi düzeltti:** beslenmemiş hâlde *yazma* çapraz-tenant `ok` **üretmezdi** —
  `transactions_tag_fk` **23503** → 500 → **kayıt kaybı**. Şema **sessiz bir ikinci ağdı** ve bir
  izolasyon ihlalini §4.6 kaybına çeviriyordu. İkisi de ölçüldü.

**🔁 En değerli bulgu bir KANIT boşluğuydu — ve bu sınıf oturumda üçüncü kez çıktı.** Güvenlik
denetçisi üretim yazma yolundaki atomik ilerletmeyi **gerçek bir TOCTOU'ya** çevirdi ve
**tüm suite yeşil kaldı**. Sebep: `internal/sun` `AdvanceCounter`'ın N yarışçıyı tek kazanana
indirdiğini kanıtlıyor, `checkin` tap'in doğru kayıtla bittiğini kanıtlıyor — ama **tap yolunun onu
ÇAĞIRDIĞINI** pinleyen hiçbir şey yoktu; mutasyon **ikisinin arasından** geçti. Yeşil kalmasının tek
sebebi `SELECT→UPDATE` penceresinin milisaniye-altı olmasıydı: 80 ms'ye genişletilince **12/12 tap
SUN-valid** oldu.
> **Doğru çözüm yarışı daha çok test etmek değil, ÇAĞRIYI PİNLEMEKTİR.** Yapıcı bunu iki kez
> ölçtükten sonra seçti (bariyerli ve bariyersiz HTTP yarışı, ikisi de bozuk şekle karşı yeşil kaldı):
> tüketici tarafında sayan bir arayüz → "tam 1 `AdvanceTagCounter`, şu tenant/uid/ctr ile, tag
> tenant'ının `WithTenant`'ı içinde". Denetçinin mutasyonu birebir kurulunca artık **RED**.
> agent-brief'e yazıldı.

**Üç ölü/yanlışlanamaz koruma daha bulundu:**
- **`sys:tap-freshness` ÖLÜYDÜ** — `tap:pageAgeSeconds` hiç beslenmiyordu. Beslendi. Bandı
  varsayılanlarla **boş** (TTL 900 == eşik 900) — **hiçbir testin yakalamamasının sebebi buydu**;
  M5-10 pencereyi daraltınca erişilebilir olacak. İki cevap **türce farklı**: TTL kaydedilmeyen 400,
  guardrail **kaydedilen reject** (§4.6).
- **N3 wiring'i yanlışlanamazdı:** harness debounce'u `DefaultParams()` ile **aynıydı** → wiring silinince
  test yeşil kalıyordu. **Dejenere değer**, "kendi kurduğu nesne"nin akrabası. Harness 120 sn'ye çekildi.
- **Dört verdict damga sınıfı derlenen CSS'e hiç girmiyordu** — Tailwind globları **Go dosyalarını
  taramıyor** ve `StampClass()` repodaki tek Go-tarafı sınıf literaliydi. `app.css` gitignore'da olduğu
  için hata yalnız **taze build'de** görünüyordu; denetçinin yöntemi bu yüzden buldu.

**Dayanıklılık (denetçi ölçümü):** 9 düşmanca girdi şekli (yıl 9999, yıl 1, ±23:59 offset, ±180
koordinat, üstel ve denormal float) → **hepsi 200 + tam bir satır, hiç 500 yok**; **politika katmanı
zorla düşürülünce kayıt YİNE yazılıyor** (`flag`/`default`). §4.6 tuttu. Dokümanlar `redirect`/`ignore`
beyan **edemiyor** → bir tenant politikası tap'i **susturamaz**.

**Ayrıca:** sıfır-zaman nöbetçisi `0001-01-01T00:00:00Z` ile çakışıyordu (denetçi **bir saniye
ötedeki pozitif kontrolle** gösterdi) → `OccurredAt` **pointer** oldu, "beyan edildi mi" ile "neydi"
artık aynı değeri paylaşmıyor.

**Yöntem tuzağı (agent-brief'e yazıldı):** çıplak `go test ./...` **her DB testini sessizce SKIP**
ediyor; yalnız `make test` gerçek Postgres'e karşı koşuyor. "0 SKIP" iddiası hangi komutla ölçüldüğü
söylenmeden anlamsız.

**Sıradaki: M5-06** (onay ekranı) — M5-05 geçici bir `pages.Result` bıraktı.

### 2026-07-31 (5. oturum) — **M5-04 done** (tap sayfası)

`cfa6cd5`. İki denetçi, üçüncü göz **RED**. **Kullanıcı kararı:** buton nötr **"Tap"** kalıyor
(yön karar motorunun işi; sayfada tahmin etmek onay ekranıyla çelişirdi — §9 gereği soruldu).

**§4.4 — kart mevcut API ile imkânsız bir şey istiyordu.** "Sayfa açılışında sayaç ilerletilmez" ama
`sun.Verify` 6. adımda ilerletiyor → yeni giriş noktası `PreviewWithoutReplayProtection`. Caydırıcılık
**isimle değil yapıyla**: `Preview` ≠ `Result` (atanamaz → `Verify`-şekilli kod **derlenmez**),
`SUNValid` alanı **yok**, ve denetimden sonra `db.ResolvedTag` da taşınmıyor — çünkü
`pv.CMACValid && p.Ctr > pv.Tag.LastCtr` **yazılabilir bir cümleydi** (§4.4'ün adıyla yasakladığı
TOCTOU) ve KEK-sarmalı anahtarı handler'a veriyordu. **İki denetçi de beş caydırıcıyı kendi
paketlerinden derleyerek denedi; hepsi derleme hatası.**

**🔬 Güvenlik denetçisi yapıcının bıraktığı boşluğu ölçümle kapattı.** Yapıcı "HTTP yolunda
geçerli-CMAC testi yok" diye dürüst bir sınır yazmıştı (gerekçe: SDM türetmesinin ikinci kopyası
M2-04'ün byte-reversal hatasını gizleyebilirdi). Denetçi tam da doğru şeyi yaptı — `internal/sun`'dan
**tek satır almadan**, repo dışında **bağımsız RFC 4493 CMAC + NXP SDM** yazıp **gerçek geçerli bir SUN
URL'i mintledi**: 30 açılış → `last_ctr` **0→0**; aynı URL `Verify`'dan geçince **700→701,
`SUNValid=true`**; replay → false. **İç-tutarlı vektörün yakalayamayacağı sınıf ilk kez dışarıdan
sınandı.**

**RED — ve bu oturumun en öğretici hatası, çünkü kimsenin dosyasında değildi.** `NewTap`
`TapLimiter`'ı **`Audit` olmadan** kuruyordu → M5-03'ün **onaylanmış** kriteri ("429 + tenant
çözülmüşse `audit_log` satırı") **üretimde ölüydü**: 15×429, red **kimliği çözülmüş**
(`employee_id=…0301`), `audit_log` **4145→4145**. M5-03 yeteneği teslim etmiş, **montajı devretmişti**;
yanlışlanan cümleler M5-03'ün **kendi dosyasında ve kartında**, doğru göründükleri hâlde duruyordu.
`cmd/tappa/main.go`'da recorder zaten vardı — `NewTap`'in imzasında parametre yoktu, yani unutulmuş
satır değil **eksik tasarım**.

> **🔁 Ve düzeltmenin ilk mutasyonu YEŞİL kaldı.** Red testleri `tp.limiter`'ı **kendileri** kurup
> `Audit`'i açıkça geçiriyordu — yani üretim montajını hiç sınamıyorlardı. Yapıcı bunu **kendi
> raporladı**: *"denetlediği şeyi kendisi kuran test hiçbir şey denetlemez."* Testler artık ürünün
> kurduğu limiter'ı **üretim bütçesiyle** sürüyor. Kapanış denetiminde **aynı tuzak bir alan yanında**
> bulundu (`Refused: nil` de tüm suite'i yeşil bırakıyordu) → o da kapatıldı. agent-brief'e yazıldı.

**İmzalı bağlam:** çipin MAC'i sunucuda **bir kez** kontrol ediliyor, sayfaya **tek bit** geçiyor;
türetilmiş anahtar + oturum id'si AAD olarak. Denetçi şemayı **kendi HMAC'iyle** yeniden kurdu ve
birebir eşleşti; **dokuz sahtecilik denemesi** reddedildi; sabit zamanlı; TTL 15 dk, ileri kayma 1 dk
fail-closed. **`ctr` ve uid bağlamda seyahat ediyor** (ikisi de adres çubuğunda zaten var), **CMAC
etmiyor** — tersini iddia eden **üç yer** düzeltildi.

**Bilinçli sapma (denetçi meşru buldu):** sayfa ön-doğrulama başarısız olsa da render ediliyor
(retired/lost/bozuk CMAC/yabancı plaket). Reddetmek butona hiç basılmaması demek ve §4.6'nın
**kaydedilmesini istediği** bir `reject` iz bırakmadan kaybolur. Yabancı-tenant senaryosunda **mekân
adı gövdede 0 kez** geçiyor (yalnız UUID'ler, opak).

**Ayrıca:** fontlar self-host (6 woff2, **79.032 bayt** — "92 KB" ölçüm hatasıydı, dizinin tamamıydı;
SKILL.md'de de düzeltildi), **mutlak URL 0** · `/static` dizin listelemesi kapatıldı · tap yanıtlarına
**CSP** · `watchPosition` **0**, çift-basış artık `preventDefault` (önce koordinatsız natif submit
oluyordu = kanıt kaybı) · `internal/domain/tenant` yeni paket, tüketici arayüzü **yalnız `WithTenant`**
tanımlıyor (RLS dışı okuma **yapısal olarak** mümkün değil).

**M5-05'in kart akışı düzeltildi:** "parse → `sun.Verify` → …" gönderilen tasarımda **çalışmaz**
(bağlamda CMAC yok → `verifyMAC` false → sayaç hiç ilerlemez). Gerçek sözleşme yazıldı. **Sıradaki:
M5-05** — milestone'un en kritik görevi; M3+M4 ilk kez gerçek bir istekle çalışacak ve **N5 orada
kapanmalı**.

### 2026-07-31 (5. oturum) — **M5-03 done** (middleware: gerçek IP, kimlik, oran sınırı)

`1fdd1ad`. `internal/httpx/{realip,identity,ratelimit}.go`. **İki denetçi, 2 RED.**

**Çözücü:** XFF **sağdan sola**, tüm başlık örnekleri boyunca; güvenilmeyen peer → başlık **hiç
okunmaz**; `TrustedProxies` boş → `RemoteAddr`. chi `middleware.RealIP` **kullanılmadı** (başlığa
koşulsuz güvenir). Gerekçe §5: IP eşleşmesi **50 güven puanı**, yani **sahtelenebilir bir adres hiç
adres olmamasından kötüdür**. Denetçi kendi 24 satırlık tablosu + 10 sahtecilik denemesi + **canlı TCP
soketi** ile sınadı: obs-fold, başlık büyük/küçük harf, 51 hop, 100k girişli zincir, 4-in-6, zone —
append eden proxy arkasında **10/10** sahtecilik taze kova satın alamadı.

**RED-1:** *"tek otorite / bir handler kazara ham başlığa uzanamaz"* **yanlıştı** — `Forwarded`
(RFC 7239), `CF-Connecting-IP`, `X-Client-IP` handler'a ulaşıyordu. Strip listesi **3→32**; canlı
soketle ölçüldü: **36 adayın 23'ü** geçiyordu → **4**. Kalan dördü **pozitif kontrol**: `Via` ve
`X-Forwarded-Host/-Proto` adres taşımıyor, **`Origin` ise CSRF kontrolleri için taşıyıcı** — silinse
aktivasyon kırılırdı. İddia denylist kapsamına indirildi (bilinmeyen satıcı başlığı hayatta kalır).

**RED-2 — ve bu ders bu oturumun en pahalısı:** varsayılan-rota kapısı **ham** prefix'e bakıyordu, ama
normalizasyon 4-in-6'yı unmap ediyor → `TAPPA_TRUSTED_PROXIES=::ffff:0.0.0.0/96` kapıya `/96` görünüp
çözücüde **`0.0.0.0/0`** oluyordu. Prod'da **ne hata ne uyarı**; sıradan bir internet çağıranı kendi
adresini yazabiliyordu. **Kapı bir önceki RED'e cevaben eklenmişti.**
> **🔁 Desen (üçüncü kez):** `HTTPS://` (M5-01) · `Cross-Site` (M5-02) · `::ffff:` (M5-03) — hepsinde
> **kontrol, tüketicinin gördüğünden farklı bir biçime bakıyordu.** agent-brief'e yazıldı.

Çözüm **ikinci temsili silmek** oldu (config v4-mapped yazımı reddediyor, httpx düşürüyor) — iki
kanonikleştirme hatanın **kaynağıydı**; ayrıca `config→httpx` **import döngüsü** yüzünden
normalizasyon config'e taşınamıyordu. Yapıcı iki alternatifi de gerekçeleyerek reddetti.

**Kart iki yerde düzeltildi (gerçekle hizalama, kaçış değil — denetçi ikisini de meşru buldu):**
klasik bir **tenant middleware'i tap yolunda KURULAMAZ** — tenant çözümlemenin **ÇIKTISI**dır (ADR 0002
md.7); girdi alan bir middleware çağıranın **kendi tenant'ını adlandırmasına** izin verirdi. Yerine
`httpx.Identify` yalnız **gerçekleri** taşıyor ve **sıfır değeri `SessionUnresolved`** (M5-01'in kutup
dersi): middleware'i unutan bir rota **gerçek bir oturumsuz tap gibi görünemez**. `BySession` o durumda
**500** veriyor (fail-open'dı, denetçi 100/100 isteğin ölçülmeden geçtiğini ölçtü) ama `SessionAbsent`
**geçiyor** — §5 satır 3 meşrudur. İkincisi: 429'da `audit_log` **yalnız kimlik çözüldükten sonra**
mümkün (`tenant_id` NOT NULL + FK) → **uydurma "sistem tenant'ı" üretilmedi**.

**M5-02'nin adlandırılmış yükümlülüğü kapandı:** `handler.clientIP` artık `httpx` üstünden çözüyor →
proxy arkasındaki çağıranlar **ayrı kova** (negatif kontrol: çözücüsüz = M5-02 hâli → B'ye 429).
`floodLimit` **600'de bırakıldı** — düzeltilmesi gereken **sayı değil anahtardı**.

**Yapıcının kendi testi kendi taslağını çürüttü:** `tapSessionLimit` ilk hâlinde 120'ydi; 5 saniyelik
bir yenileme döngüsü tam 120 eder → 300'e çıkarıldı. Aynı sınıf: "meşru akış sınıra değmez" cümlesi
ölçülmeden yazılmamalı.

**Kapsam:** `TapLimiter` yazıldı ve testlendi ama **monte edilmedi** (`/t`, `/api/checkin` yok) —
montaj sırası bir **sözleşme** ve 429'un §4.6 kalıntısı adıyla yazıldı (aşağı "M5-04/M5-05'e
devralınan"). **N5 yalnız oturum yarısı** teslim edildi; `sys:tenant-mismatch` hâlâ ölü ve **"kapandı"
denmiyor** — tag yarısı M5-05.

**Sıradaki: M5-04** (tap sayfası, skill `tappa-brand`) — bağımlılıkları tamam.

### 2026-07-31 (5. oturum) — **M5-02 done** (davet + aktivasyon, iki fazda)

Görev büyük olduğu için **ikiye bölündü**: A fazı veri katmanı (`9139ee7`, agent `tappa-db-migrator`),
B fazı akış + arayüz (`0601b6d`). Toplam **5 denetim turu, 3 RED**. Ayrıca `00010 locations.wifi_ssid`
(Q14 WiFi adımı için — kart "ağ adı lokasyon kaydından gösterilir" diyordu ama **öyle bir alan yoktu**).

**Kullanıcı kararları (2026-07-31):** GDPR saklama süresi **config'den** gelsin, koda hukuki sayı
gömülmesin (→ `TAPPA_RETENTION_YEARS`, Q13 hukukçu onayı **backlog B3**) · WiFi ağ adı için
**lokasyona alan eklensin** (→ 00010).

**Üç RED — üçü de oturumun tekrarlayan sınıfı** (*dosya/kart, sağlamadığı garantiyi beyan ediyor*):
1. **A:** kart, `code_hash` biçim CHECK'inin **kod entropisini zorladığını** ve kısa koda geçişin CHECK'i
   tetikleyeceğini yazıyordu. Ölçüm: `sha256('123456')` da 64-hex → **tel-tuzak hiç ateşlenmez**.
   Yükümlülük düzeltme bloğundan **Kabul kriterleri**ne taşındı (≥128 bit değilse kilitleme **zorunlu**,
   ve *hiçbir mekanik kontrol bu geçişi yakalamaz* açıkça yazıldı).
2. **B:** **aktivasyon-fixation.** `SameSite=Lax` çerez **gönderimini** kısıtlar, **yazılmasını değil.**
   Çapraz-site GET saldırganın kodunu kurbanın tarayıcısına ekiyor → sonraki aynı-site GET **başka
   tenant'ın** formunu render ediyor → `Submit` mevcut oturuma **hiç bakmadığı** için kurbanın oturumu
   **sessizce eziliyor**. Oysa dosya "en kötüsü saldırganın zaten elindeki kodu harcamaktır" diyordu.
3. **B:** **sınırsız, SİLİNEMEZ `audit_log` yazımı.** İki dal her istekte satır yazıp hiçbir pencereyi
   artırmıyordu: 300 istek → 290×429 **ama 300 satır**. `audit_log` DB seviyesinde append-only —
   **`tappa_owner` bile silemiyor** — ve ön koşul yalnızca *bir ölü davet linki* (her aktive çalışanın
   WhatsApp'ında duran kendi linki). **§4.6'nın koruduğu iz, kendi bağışıklığı yüzünden silah oluyordu.**

**Bunu düzeltmek dördüncü bir sorunu açığa çıkardı** (denetçi YÜKSEK/bloklamayan dedi, yine de kapatıldı):
tek per-IP penceresi **başarıları da** reddediyordu → 60 bilinmeyen kod, geçerli bir aktivasyonu
kilitliyordu. `clientIP` `X-Forwarded-For` okumadığı için ters proxy arkasında **her istek tek anahtarı
paylaşır** → tek çağıran tüm ürünü kapatabilirdi. Çözüm **üç bütçe, üç iş**: `flood` (yalnız bu
geçerli bir aktivasyonu reddedebilir) · `unknown` (yalnız **süreç log'unu** sınırlar) · `invite`
(yalnız **`audit_log`'u** sınırlar). Ölçüm: 300 istek → **11 satır**, 60 bilinmeyen kod sonrası geçerli
kod **303 servis edildi**, 605. istek yine **429**.

**Ders — ayrım cümlenin içinde saklıydı:** *"meşru akış sınıra yapı gereği değmez"* akışın **kendi
katkısı** için doğru (200 ardışık başarılı aktivasyon → sıfır 429), akışın **servis edilip edilmediği**
için yanlış. İki yarı ayrı ayrı yazıldı.

**Yapısal kararlar (hepsi ölçümle, tercihle değil):**
- **Kaynaşık CTE** `ConsumeInviteAndActivate`: iki ayrı sorgu "aktive ⇒ davet tüketildi"i **çağrı
  sırasına** bırakıyordu (hayalet-çalışan). Kaynaştırmak **daha ince bir hatayı** ortaya çıkardı:
  veri-değiştiren CTE **koşulsuz** çalışır → deaktif çalışanda davet **yanıyordu** (COMMIT ile ölçüldü).
  İç CTE'ye `EXISTS` guard'ı → `burned=f`. **Yapısal > disiplin.**
- **`FOR SHARE` ölçümle reddedildi:** kalan dar yarışı kapatırdı ama iki cihaz aynı çalışanı aktive
  ederken **40P01 deadlock** üretiyor. Harcanmış bir kod, 500'den iyidir.
- **Sütun-düzeyi `GRANT UPDATE (used_at)`:** dosya "tablo-geneli UPDATE **şart**" diyordu; ölçüm
  çürüttü. Diriltme (`expires_at`), kaydırma (`employee_id`), hash-yeniden-yazma → **permission denied**.
- **Alan ayrımı çift:** ayrı anahtar **ve** etiketli girdi. Aynı anahtar altında bile invite MAC ≠
  session yapısı (ölçüldü); `config.Load` iki anahtarın eşitliğini reddediyor (gerçek arıza: tek
  `openssl rand` çıktısının iki yere yapıştırılması).
- **M5-01'in RED'i B fazında yeniden üretilmişti** (`activationState.code` çıplak `string` → `%+v` ham
  kodu bastı). `invite.Code`'a çevrildi. **Kalıp bir kez yazılınca bitmiyor, her yeni pakette tekrar
  düşülüyor.**

**redline R7 `code_hash`'i taramıyordu** → eklendi (kapsam genişlemesi). Sebep: `code_hash` bu tasarımda
**taşıyıcı (bearer) kimlik bilgisi** — hash tenant'ı çözer ve daveti harcar, yani loglanması kodu
loglamakla aynı. İlk desen *kelime* eşleştirip masum satırları (`"code expired"`) kırmızıya
döndürüyordu → **değer** hedefleyecek şekilde yeniden yazıldı: yanlış pozitif üreten kural gevşetilir,
gevşetilirken gerçek dal da gider.

**Toplam 7 aşırı iddia ölçümle çürütülüp indirildi.** Kalan sınırlar (fail-open `Sec-Fetch-Site`,
`ConfirmView.Code`'un tip korumasız oluşu, çerez gölgeleme, süreç-içi limiter) **sınır olarak** yazıldı.

**Kapsam dışı bırakılanlar (bilinçli):** davet üreten HTTP uç noktası **yok** (admin auth M6-01;
kimliksiz uç nokta Y-D'yi genişletirdi) · **§5 satır 3 bağlanmadı** (hedef var, `transactions` yazmıyor;
yönlendirme M5-04) · fontlar self-host değil (M5-04) · M5-03 middleware · M5-07 tur/practice.
**Sıradaki: M5-03** — gerçek IP devri adıyla yazıldı ("M5-03'e devralınan" md. 1).

### 2026-07-31 (5. oturum) — **M5-01 done · M5 başladı (1/10)**

`internal/session` teslim (`a71e1b2`): token üretimi/doğrulama/yenileme/iptal + çerez kodeği + 5
tenant-kapsamlı sqlc sorgusu. **Migration yazılmadı** — `sessions` 00003'te RLS beşlisi ve
`REVOKE DELETE` ile zaten tam; uygulanmış migration'a dokunulmadı (§6).

**Beş tur sürdü. İki RED ve ikisi de AYNI SINIFTAN — bu oturumun asıl çıktısı bu ders:**
> **Bir yorum "hiçbir çağıran X yapamaz / yapısal olarak imkânsız" diyorsa, X harici bir paketten
> DENENMİŞ olmalıdır. Denenmediyse iddia değil, *sınır* yazılır.**
1. **RED-1 (genel denetçi):** `Token` **unexported bir alanda** taşındığında `fmt`
   `Formatter/Stringer/GoStringer/LogValuer`'ı **atlıyor** (`CanInterface()==false`) ve `%v/%+v/%#v` +
   `slog` **ham token'ı** basıyordu — oysa `token.go` bunun "yapısal olarak imkânsız" olduğunu yazıyordu
   ve testi yalnız **exported** alanlı sarmalayıcıyı deniyordu (bu yüzden yeşildi ve hiçbir şey
   kanıtlamıyordu). Fix: `type Token struct{ v *string }` → dolaylılık adres bastırır. XOR-maske
   alternatifi §7 ("paket seviyesi singleton yok") gerekçesiyle reddedildi; `func`-alan comparability'yi
   bozardı. **Bedeli kaydedildi:** `==` artık **kimlik**, değer değil (sonuçları fail-closed).
2. **RED-2 (`tappa-security-auditor`):** `cookie.go` *"no caller can produce a non-Secure session cookie
   in production"* diyordu; ama **Go'da yasak olan alanı ADLANDIRMAKTIR, `T{}` yazmak değil** →
   `var c session.Cookies` (paketin **kendi** harici testinin kullandığı idiom!) `secure=false` verip
   `Set` **ve** `Clear`'da Secure'suz çerez yazıyordu. Fix: **kutup çevirme** → `struct{ insecure bool }`,
   sıfır değer **fail-closed**. Tehlikeli durum artık *kazara temsil edilemez*.

**Denetim derinliği (hepsi denetçilerin kendi komutlarıyla, yapıcının testlerinden bağımsız):**
`Verify`'ın **tek sorgu** oluşu gerçek Postgres'te **iki ayrı yolla** (`pg_stat_user_tables` deltası +
marker'lar arasına bracket'lenmiş `log_statement=all`) · RLS izolasyonu **üç denetçi tarafından ayrı ayrı**
`ALTER TABLE sessions DISABLE ROW LEVEL SECURITY` → test RED → geri açıp `pg_class` ile ölçerek doğrulama ·
sızıntı için **306 hücrelik matris + pozitif kontrol** (çıplak `string` aynı harness'ta 18 render'ın
13'ünde sızdı → sonda kör değil) · **17 sıfır-değer yolu** (embedded alan, kanal, `reflect.Zero`,
map-eksik-anahtar, `append` büyümesi…) × `Set`+`Clear` · **99 `Env`×`BaseURL`** kombinasyonu ·
sqlc çıktısının bağımsız yeniden üretilip **bayt bayt** diff'lenmesi (elle düzenleme yok).

**§5 satır 3 vs satır 4 — kartla gerçeğin çeliştiği yer, gerekçeli çözüldü.**
`resolve_session_by_token_hash` `employees.status` **döndürmüyor** (00003:189-205) → kartın "tek sorgu"
+ "deaktive anında geçersiz" ikilisi literal olarak sağlanamaz. Session katmanı **gerçeği taşır, karar
vermez**: `Verify` iptalde **dolu `Resolved` + `ErrRevoked`** döndürür ki çağıran §5 satır 4'ü (reject +
**KAYIT** + uyarı) uygulayabilsin. Aksi hâlde satır 4 satır 3'e (aktivasyon, **kayıt yok**) çökerdi ve
deneme iz bırakmadan kaybolurdu (§4.6). Guardrail sırası bunu doğruluyor: `sys:no-session` **#6**,
`sys:employee-deactivated` **#7**. Kart bir "Kart düzeltmesi" bloğuyla düzeltildi — kriter
**zayıflatılmadı, yeri değişti** ve düşürülen garantinin karşılığı üç maddeyle gösterildi.

**Kapsam genişlemesi (bilinçli, işaretli):** `TAPPA_ENV` kapalı küme `{dev,staging,prod}` oldu
(`internal/config`) — `NewCookies` artık `IsProd()` okuduğu için env bir **güvenlik niteliği**;
`TAPPA_ENV=production` insana doğru görünür ama `IsProd()`'u false yapar. Değiştirmeden önce mevcut tüm
değerler tarandı (`.env`, `.env.example`, Makefile, compose, CI → hepsi `dev` veya unset) — hiçbir şey kırılmadı.

**Ayrıca:** `DeviceInfo` sınırlandı (≤64 rune, geçerli UTF-8, Cc/Cf/Zl/Zp **ham dizede, trim'den ÖNCE**
reddedilir → konum önemsiz); **reddeder, kırpmaz** (sessiz kırpma = sessiz kabul, §7). Yapıcının ilk
taslağı 120'ydi ve **kendi testi dekoratif olduğunu yakaladı** (gerçek Chrome UA 117 rune, altından
geçiyordu).

**Kalan:** Q11 **AÇIK** (gerçek iPhone — "Bekleyen kullanıcı eylemi"). 7 devir notu →
"M5-02/M5-03'e devralınan"; en önemlisi **deaktif çalışanın çerezi canlı oturum olarak çözülmeye devam
eder** → tap dışındaki her kimlik yüzeyi `employees.status`'ü kendisi kontrol etmeli.
**Sıradaki: M5-02** (davet + aktivasyon; §5 satır 3 oraya bağlanıyor).

### 2026-07-26 (4. oturum, compact sonrası) — **M4-07 done · 🏁 M4 KİLOMETRE TAŞI TAMAM (7/7)**

**M4-07 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor R6/R8). `c5536be`: `table_test.go`
duplikasyon-ledger'ı ile §5 yedi satır + 5 zorunlu ek vaka. **Merkez:** debounce KİŞİ-bazlı vaka (farklı
kişiler aynı plaket 10sn→hepsi ok) — person-scoping'i nötrleyen mutasyon 3 satırı RED yaptı. R8: Evaluate tek
çağrı, erken-return yok (redline temiz). R6: row7-flag, no-session-tek-redirect, default→flag. isPracticeTap
sertleştirildi (+LastOpenIn==nil, kayıt yazımını etkilemez). %96.7 kapsam.

**🏁 M4 TAMAM (7/7):** internal/geo (haversine, DST-siz) · Input/Decision tipleri (saf) · Decide bağlam+delege
(§5 tablo motora, if-zinciri yok) · yön (son-açık-giriş toggle, gece vardiyası) · vardiya+geç kalma (DST doğru,
rapor-only) · trust/QR/practice (sunucu-türev, exploit kapalı) · tablo test (%96.7). Tüm motor saf
(time.Now/DB/HTTP yok, Now girdiden); policy motoru (M3) üstünde durur, karar taklidi yok.

**🔴 M5 için BLOKLAYAN + devirler (ŞU AN "M4/M5'e devralınan"da):** N5 tenant-mismatch (Input'a TagTenantID/
SessionTenantID, sys:tenant-mismatch besle — çapraz-tenant deliği), N1 tap:sunValid, N2 channel sunucu-türetimi,
N3 debounce→Params, N4 Decision→sütun sadakati, ErrUnknownTag logla, manuel entered_by (M5-05). Ayrıca düşük:
guardrails.go:222 yanıltıcı yorum → internal/policy sonraki dokunuş.

**Sırada:** M5-01 (internal/session — yeni kilometre taşı; M5 kartını baştan oku). **Milestone sınırı.**

### 2026-07-26 (4. oturum, compact sonrası) — **M4-06 done** (trust + QR + practice)

**M4-06 done — üçüncü göz 1. turda ONAY** (2 mutasyon öldürüldü). `a82dfa8`: Trust `20+50(IP)+30(GPS)`
verdict switch ÖNCESİ, **verdict'ten bağımsız** (reject 70 > ok 50 kanıtı). **Practice sunucu-türev**
(`ActivatedAt`+`LastForPerson==nil`); Input'ta client practice alanı YOK (reflection guard denetçi tarafından
Input'a alan eklenerek RED kanıtlandı); checkout asla practice → **saat-şişirme exploit'i yapısal kapalı**.
QR uçtan uca policy'de (base:qr-requires-ip): QR+IP-yok+GPS-var→flag (Q15, GPS tek başına kurtarmaz), QR+IP→ok,
SUN-suz QR sys:sun-invalid'e takılmaz. Manuel SUN atlar; entered_by write-path → M5-05 kartına kriter eklendi
(Decide saf func hata dönemez). %96.7 kapsam.

**Bloklamayan sertleştirme (→M4-07):** `isPracticeTap` yalnız LastForPerson==nil'e bakıyor; tutarsız çağıran
(LastOpenIn!=nil, LastForPerson==nil) checkout'u practice yapabilir — **client-erişilemez, tutarlı M5 sorgusundan
doğamaz**; opsiyonel: isPracticeTap'e `LastOpenIn==nil` ekle (resolveDirection'ın stale-practice guard'ını yansıtır).

**Sırada:** M4-07 (tablo bazlı test seti — §5 yedi satır + zorunlu ek vakalar [debounce KİŞİ-bazlı!], kapsam %90+,
iki denetçi R6/R8). **M4'ün son görevi.**

### 2026-07-26 (4. oturum, compact sonrası) — **M4-05 done** (vardiya çözümü + geç kalma, DST)

**M4-05 done — üçüncü göz 1. turda ONAY.** `63f6b4a`: `Decide` geç kalmayı `Input.Shift`+Now+`time.LoadLocation
("Europe/Malta")` ile hesaplar (departman/lokasyon çözümü çağıranın — M4-02 sözleşmesi). **DST denetçi tarafından
BAĞIMSIZ yeniden hesaplandı** (Python zoneinfo): mart 09:15→15, ekim 09:20→20, overnight 01:00→420; naif
midnight+offset bug'ı (−45/80) mutasyonla yakalandı. tzdata tap paketine gömülü (tek binary scratch image).

**Geç kalma RAPOR-only:** `Decision.MinutesLate *int` (nil=hesaplanmadı; **int dakika, float YOK** §6); Evaluate
SONRASI hesaplanır, context'e GİRMEZ, hiçbir baseline/guardrail `time:minutesLate` okumaz → **verdict'i etkilemez**
(180-geç→OK). Yalnız check-IN'de (checkout asla geç). Çapraz-lokasyon Q17: `employee:crossLocation`→base:cross-
location-note + `Decision.CrossLocation` (geç damgası yok). Shift==nil VE boş-tz→nil (LoadLocation("")→UTC tuzağı
guard'lı). %96.4 kapsam. cmd/tappa dokunulmadı.

**Sırada:** M4-06 (trust 20+50+30, QR base:qr-requires-ip, practice sunucu-türetimi saat-şişirme exploit'i).

### 2026-07-26 (4. oturum, compact sonrası) — **M4-04 done** (yön tayini in/out)

**M4-04 done — üçüncü göz 1. turda ONAY** (4 mutasyon öldürüldü). `703d3d1`: `Decide` `Decision.Type`'ı
`Input.LastOpenIn`'e göre saf toggle (açık giriş→out, yok→in). **Takvim-günü filtresi YOK** — denetçi
bağımsız cross-midnight/ay/yıl/artık-gün testleriyle + 5h farkın 400 gün boyunca sabit kaldığıyla kanıtladı
(gece vardiyası bug'ının kaynağı bu filtredir). Stale open-in **>18h** → out + note (asla sessiz in; strict >).
Practice LastOpenIn → in muamelesi (eğitim tap'i gerçek check-in'i açık tutamaz — M4-06 saat-şişirme exploit'i;
asıl dışlama M5 sorgusunda). Type yalnız ok/flag'te (reject/ignored/redirect→nil). UTC saf süre, sabit-Now.

**Sırada:** M4-05 (vardiya çözümü + geç kalma — departman>lokasyon, çapraz-lokasyon Q17, DST Malta, geç kalma
verdict'i etkilemez, float değil).

### 2026-07-26 (4. oturum, compact sonrası) — **M4-03 done** (Decide: bağlam kurma + kararın uygulanması)

**M4-03 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor R8; §4.6/§5, kırmızı çizgi ihlali yok).
`bfbbf77`: `tap.Decide` gövdesi — Input→policy.Context (ipMatch/gpsMatch/gpsDistanceM/gpsConflict/ctrGap +
guardrail alanları) → `policy.Evaluate` **tek çağrı, if-zinciri/erken-return YOK** → effect→verdict. no-session→
redirect+kayıt-yok (§5.3 tek istisna); row7→flag; boş set→flag (§4.6 sessiz-ok yok). %95.7 kapsam.

**R8 mutasyonla + kod-okumasıyla:** deactivated+invalid-SUN → sun-invalid kazanır + **Security=false** (deaktivasyon
sızmaz, push seli yok); üçüncü göz erken-return ekleyip RED gördü. **Marker-hilesi iki yönlü doğrulandı:**
SessionTenantID=Employee!=nil işareti (sys:no-session sürücüsü); TagTenantID nil → sys:tenant-mismatch ilk-clause
kısa-devre, gerçekten inert (misfire yok). PolicySet Input alanı olarak eklendi (imza korundu); Decision'a
MatchedSid/Layer/PolicyVersionID (M3-07). M4-02 kartı düzeltildi. Type/Trust/lateness/Practice M4-04/05/06'da.

**🔴 M5 için BLOKLAYAN devir (N5, ŞU AN'a yazıldı):** tag çözümü context-less (ADR 0002 md.7) → RLS çapraz-tenant
tag'i çözümde gizlemez. Decide tenant-farkındalıksız (Input'ta tenant id yok) → çapraz-tenant tap `sys:tenant-mismatch`
beslenmezse `ok` yazılır (izolasyon deliği). M4-03 doğru şekilde erteledi (karar taklidi yapmıyor); **M5 Input'u
TagTenantID/SessionTenantID ile genişletip guardrail'i ateşlemek ZORUNDA — tek gerçek engel.**

**Sırada:** M4-04 (yön tayini — son açık girişe göre toggle, gece vardiyası, practice zincire girmez).

### 2026-07-26 (4. oturum, compact sonrası) — **M4-02 done** (karar girdi/çıktı tipleri)

**M4-02 done — üçüncü göz 1. turda ONAY.** `860fcd8`: `internal/domain/tap/{types,decide}.go`. `Input`
(14 alan) + `Decision` (9 alan) karta birebir; `Decide(Input) Decision` imzası sabit, gövde M4-03
**panic-stub** (zero-value dönmez → §4.6 sessiz-onay tuzağı yok). **Saflık kanıtlandı:** paketin kendi
import'ları `net/netip,time,internal/geo,uuid` — store/db/sun/database-sql/http/pgx KODDA yok, `time.Now()`
çağrısı yok; `math/rand`+`database/sql/driver` yalnız uuid transitifi (policy ile birebir aynı). Enum'lar
typed (migration CHECK sözlükleriyle birebir: nfc/qr/manual, ok/flag/reject/ignored, in/out, active/retired/
lost, invited/active/deactivated). **`Employee` pointer (§5.3 nil=oturum yok) + `Status` (§5.4 deactivated)
ayrı** → iki farklı karar mümkün. tap kendi `SUNResult`'ı (sun.Result db/store/pgx sürüklediği için import
edilmedi; M5 map eder). Sapma (meşru): `Employee.ActivatedAt` — Practice **sunucu-türetim** kaynağı
(Input'ta client practice bool'u yok → M4-06 exploit'i önlenir).

**Sırada:** M4-03 (Decide gövdesi — bağlam kur, policy.Evaluate çağır, effect→verdict). Açık nokta: Decide
policy.Set'i nasıl alacak (M4-02 imzası Set içermiyor) — M4-03 çözer + kartı düzeltir.

### 2026-07-26 (4. oturum, compact sonrası) — **M4-01 done** (internal/geo) · M4 başladı

**M4-01 done — üçüncü göz 1. turda ONAY.** `f791f91`: `internal/geo` saf paket (yalnız `math` import).
`Point{Lat,Lng}`, `Distance` (haversine, R=6371008.8 IUGG ortalama, **atan2** formülü → acos domain-NaN
tuzağı yapısal olarak yok), `WithinRadius(a,b,radiusM)` yarıçap **parametre** (config besler; §5 satır 6
"GPS < 150 m" gereği **strict `<`** → tam 150 m dışarıda). Kullanıcı M3 sonrası "M4'e devam et" dedi.

**Denetçi bilinen mesafeleri BAĞIMSIZ yeniden hesapladı** (kendi Python haversine, R aynı): St Julians→
Paceville 783.5570309985226 m, Hamrun→Msida 1115.5938858223842 m, 0 m, antipot π·R — hepsi byte-identical
(iç-tutarlı golden değil dış hesap). lat/lng-swap direnci + %100 kapsam **mutasyonla** RED kanıtlandı
(swap→761.77; Distance sabit→testler RED). §4.7 koordinat loglanmıyor; geo config/policy import etmiyor (saf).

**Sırada:** M4-02 (karar girdi/çıktı tipleri — Input/Decision struct, saf imza, Employee==nil ≠ deactivated).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-09 done · 🏁 M3 KİLOMETRE TAŞI TAMAM (9/9)**

**M3-09 done — üçüncü göz 1. turda ONAY** (12 kabul kriteri). `0c0feb4`: `docs/adr/0005-kabul-edilen-riskler.md`
— policy motorunun + dört kanıtın çözemediği 6 riski yazılı kabul (buddy punching, sahte GPS, URL biriktirme,
mekânda proxy, müdürün kimlik basması, plaket devri); her biri neden+tespit sinyali+görev+satış. Denetçi
referanslanan 8 sid + 2 anahtarı kodda grep'leyip GERÇEK olduğunu, handoff §2 tutarlılığını doğruladı.

**🏁 M3 TAMAM (9/9):** ADR 0004 (motor modeli) · policy şeması (00007, append-only) · belge modeli+doğrulama ·
değerlendirici (saf, guardrail terminal, deterministik) · **10 guardrail** (§4, sıra normatif, R8 sömürüsü
kapalı) · Tappa baseline (8 tap + 2 authz ifadesi, fail-closed lockout çözüldü) · kararın kayda bağlanması
(00008, delil zinciri) · **gevşetilemezlik özellik testi** (hiçbir tenant politikası guardrail'i allow'a
çeviremez) · ADR 0005 (kabul edilen riskler). Her görev builder→üçüncü göz; kırmızı çizgi görevlerinde
(M3-02/05/07 + M3-08) **iki denetçi** (+ tappa-security-auditor). policy kapsamı %98.3. Kullanıcı kararı:
manager employee:deactivate (followup `a6c41dd`). **Tüm kripto/DB/policy stdlib + mevcut dep — yeni dep yok.**

**M4/M5'e devreden (ŞU AN'da):** N1 tap:sunValid set · N2 channel sunucu-türetimi · N3 debounce Params'a bağla ·
N4 Decision→sütun sadakati (M5-05) · ErrUnknownTag güvenlik olayı logla. **Bekleyen kullanıcı kararı: yok.**

**Sırada:** M4-01 (internal/geo — yeni kilometre taşı; M4 kartını baştan oku). **Milestone sınırı — kullanıcı
inceleme molası verebilir.**

### 2026-07-26 (4. oturum, compact sonrası) — **M3-08 done** (gevşetilemezlik kanıtı)

**M3-08 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor; guardrail bypass + sys: sızıntısı
özellikle arandı, bulunamadı). `c39ccae`: `internal/policy/{property,invariants}_test.go` — **üretim kodu
DEĞİŞMEDİ**. Merkezî **özellik testi**: fixed-seed (20260726) 2000 iter, hiçbir rastgele tenant/baseline
politikası guardrail deny/ignore/redirect'i allow'a çeviremez. **Non-vacuous** iki yolla: (1) iterasyon-başına
guardrail-siz kontrol allow assert eder (üreteç gerçek düşman belge üretiyor), (2) üçüncü göz katman sırasını
bozunca **step-3 property assertion'ında** RED (sanity guard'da değil). security-auditor bağımsız 7-guardrail
bypass sondası koştu (retired/lost/sun-invalid/deactivated/tenant-mismatch/no-session/person-debounce'a karşı
allow+resource `*`, en spesifik location/rusty-bar dahil) → hepsi guardrail effect'inde kaldı.

**Invariant testleri (guardrail değil, ayrı):** §4.6 kanıt-yok→review (2 yığın: tam baseline + hiç politika);
§4.1 **yüzey-kilidi** (24 anahtar + 8 Context alanı birebir; üçüncü göz key ekle→25vs24 RED, field ekle→9vs8 RED).
**D1 sapması:** §4.1 testi başta biyometrik-terim denylist'iydi ama redline **R1 biyometri tarayıcısı `_test.go`'yu
da tarar** → FAIL etti; R1'i düzeltmek (tracked araç) make check git-diff kapısını kırardı (commit yasak) → test
yapısal yüzey-kilidine çevrildi (yasak terim adı geçmez, kart gereğini karşılar). Kapsam %98.3.

**Sırada:** M3-09 (ADR 0005 kabul edilen riskler — M3'ün SON görevi; ADR, kod yok; tek genel üçüncü göz).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-07 done** (kararın kayda bağlanması)

**M3-07 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, §4.3, kırmızı çizgi ihlali yok).
`1f144b7`: migration 00008 — transactions'a `policy_version_id`/`matched_sid`/`policy_layer`/`policy_context
jsonb`. Yapıcı = agent `tappa-db-migrator`. İki denetçi **sıralı** koşuldu (DB mutasyon sondası).

**§4.6 kayıt-kaybı riski (en kritik) — iki denetçi de temiz buldu:** yapıcı bir consistency CHECK ekledi
(version NULL ⟺ guardrail/default; her decided satır sid taşır). Risk: CHECK meşru bir tap'i reddederse kayıt
kaybolur. Kanıt: Evaluate baseline/tenant kararında `PolicyVersionID`'yi **daima non-nil** (`&vid`) döndürür
(evaluate.go:325-333) → CHECK meşru hiçbir kararı reddetmez; yalnız Evaluate'in asla üretemeyeceği wiring-bug
şekillerini keser (00005 verdict/channel CHECK precedent'i). Her denetçi Evaluate'in 6 meşru şeklini canlı
INSERT'le CHECK'ten geçirdi. **§4.3:** yeni sütunlar belt1 (REVOKE sütun-seviyesi f) + belt2 (trigger DISABLE→
superuser UPDATE başarılı kanıtı). **Delil zinciri:** FK `ON DELETE RESTRICT` — cited version append-only
trigger DISABLE'ken bile silinemedi (RESTRICT'in FK olduğu kanıtı). §4.5 composite FK (23503) + RLS t/t.
§4.7 policy_context mesafe (tap:gpsDistanceM), ham koordinat değil. sqlc InsertTransaction additive.

**Bloklamayan → M5-05 devir (N4, ŞU AN'a yazıldı):** CHECK+FK'nin §4.6 güvenliği M5-05'in Decision→sütun
sadakatine bağlı (baseline'ı uuid.Nil version ile yüklerse FK 23503→kayıt kaybı; Evaluate bugün üretmez).

**Sırada:** M3-08 (test seti + gevşetilemezlik kanıtı — özellik testi: hiçbir tenant politikası guardrail'i
allow'a çeviremez; guardrail sıra testi; invariant testleri §4.6/§4.1; kapsam %90+; iki denetçi).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-06 done** (Tappa Baseline yönetilen politikası)

**M3-06 done — üçüncü göz 1. turda ONAY.** `a9b4dc6`: `internal/policy/baseline.go` — kanonik Tappa
baseline. **8 `base:*` tap ifadesi** (§5.6-7 + boşluklar: qr-requires-ip Q15, gps-only-allow Q16,
cross-location-note Q17, queued-window Y7, ctr-gap-review Q21, gps-conflict-review Y-E) + **2 yetki ifadesi**.

**Fail-closed lockout çözümü (kartın kritik kabul kriteri):** ADR §3 authz→deny varsayılanı yeni tenant'ta
herkesi panelden kilitlerdi. İnce nokta: `sys:policy-edit-owner-only` guardrail'i yalnız non-owner'ı reddeder,
owner'da ATEŞLEMEZ → baseline allow olmasa owner kendi policy:edit'inde default deny'ye takılırdı. Çözüm:
`base:authz-owner` (owner→6 eylem incl policy:edit), `base:authz-manager` (manager→report:export/tap:approve/
record:manual/record:review; employee:deactivate + policy:edit HARİÇ). Roller admin_users {owner,manager}
CHECK'ini yansıtır. Denetçi bunu mutasyonla kanıtladı (owner'dan policy:edit çıkar→owner default deny).

**Diğer:** `base:` ad alanı rezervi validate.go'ya eklendi (tenant layer, case-insensitive — sys: kalıbı);
base:ctr-gap-review kaynak-kapsamlı (yoğun şube override edebilir, Q21); guardrail dokunulmaz (allow-all tenant
altında retired/deactivated→guardrail deny kazanır); ignore/redirect yok; BaselineVersion + otomatik-güncelleme-
yok; **M3-06'da DB yazma YOK** (kanonik kaynak kod'da, M7-03 tenant başına materyalize eder). baseline.go %100.

**Kullanıcı kararı (2026-07-26):** manager `employee:deactivate` **yapabilir** ("Manager da deaktive edebilsin").
Followup `a6c41dd`: `base:authz-manager`'a `employee:deactivate` eklendi (allow), testler güncellendi; `policy:edit`
manager'da HÂLÂ yok (guardrail sys:policy-edit-owner-only terminal + grant'ta yok). Odaklı üçüncü göz ONAY
(non-vacuous: action'ı çıkar→test RED; owner/roleless değişmedi; guardrail etkilenmedi). policy %98.3.

**Sırada:** M3-07 (kararın kayda bağlanması — transactions'a policy_version_id/matched_sid/policy_layer/
policy_context, EK migration 00008, agent tappa-db-migrator + iki denetçi). **Şu an arka planda WIP.**

### 2026-07-26 (4. oturum, compact sonrası) — **M3-05 done** (guardrail politikaları — §4 EN KRİTİK)

**M3-05 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi ihlali yok). `e51504b`:
`internal/policy/guardrails.go` — 10 `sys:*` guardrail TEK sıralı slice, kodda gömülü, DB'de değil, **devre
dışı bırakma API'si YOK**. İki denetçi **sıralı** koşuldu (M3-02 dersi). policy kapsamı **%98.2**.

**R8 sıra sömürüsü kapalı — mutasyonla kanıtlandı:** sun-invalid(3) < deactivated(7) < debounce(8). Üçünü
eşleştiren bağlamda sun-invalid kazanır, deny, **SecurityAlert BOŞ** → forge'lu SUN deaktivasyon durumunu
sızdıramaz, müdüre push seli yollayamaz, replay `ignore`'a yutulmaz. `TestGuardrails_OrderIsLoadBearing`
non-vacuous (yanlış sırada sızıntı geri geliyor); üçüncü göz deactivated'ı öne taşıyıp RED gördü.

**Tasarım (iki denetçi kabul):** guardrail girdileri 24 belge anahtarı dışı → **tipli Context alanları**
(SessionTenantID/TagTenantID/SecondsSincePersonLastTap/Reviewer+SubjectID), sunucu-türetimi, belge sözlüğü
DIŞI (tenant set edemez), additive (M3-04 testleri geçer), nil=güvenli (§4.6 kayıt düşmez). Güvenlik uyarısı
= `Decision.SecurityAlert` sabit sözlük (lost-tag-tapped/deactivated-employee-tapped), yalnız guardrail
ateşleyince, §4.7-temiz (değer/GPS/sır taşımaz). **config aralık kontrolü:** GPS 25–1000/debounce 30–300
başlangıçta (TAPPA_GPS_RADIUS_M=20000000 + GPS=5 artık reddedilir — proof-of-place tek env ile kapatılamaz),
guardrail+config **tek sabit kaynağı**. **Bounded-param 3 anahtar** dolduruldu (M3-03 kancası; occurredAtSkew
dahil — M3-03'te kaçırılan). evaluate.go:169 Türkçe yorum İngilizce'ye çevrildi.

**3 bloklamayan not → M4/M5 devir** (guardrail girdi sözleşmesi, ŞU AN'a yazıldı): N1 M5 her NFC tap'te
`tap:sunValid` set etmeli; N2 `channel` ctr/cmac'ten sunucu-türetimi (istemci `qr` diyip SUN atlayamamalı);
N3 `TAPPA_DEBOUNCE_SECONDS` henüz `policy.Params`'a bağlanmadı (değer akışı M4/M5).

**Sırada:** M3-06 (Tappa Baseline yönetilen politikası — 8 `base:*` ifade).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-04 done** (değerlendirici — motorun doğruluk çekirdeği)

**M3-04 done — üçüncü göz 1. turda ONAY.** `de831e1`: `internal/policy/{evaluate,conditions}.go`, **%97.9
kapsam** (evaluate.go %100). Saf `Evaluate(Set,Context) Decision` — M3-03 tipleri üstünde. Guardrail'ler
**sıralı+terminal** (kod-inşa closure, sys: + ignore/redirect serbest; M3-05 `Set.Guardrails`'i doldurur);
baseline+tenant en-kısıtlayıcı-kazanır + spesifik-resource tie-break; varsayılan tap:record→review / diğer
6→deny; bilinmeyen-op eval→ifade inert (deny koşulsuzlaşMAZ) + injectable anomaly sink (nil→slog); eksik
anahtar≠false; deterministik.

**Denetçi non-vacuous'u 3 mutasyonla kanıtladı** (terminal, restrictiveness, bilinmeyen-op) + kendi
adversaryel testleriyle terminalliği yan-etki-sayımıyla (OnAnomaly calls==0), determinizmi 1000-koşuyla,
§4.7 anomaly hijyenini kötü bağlamla doğruladı. **Kartın iki düzeltmesini onayladı:** denetçi ADR §3'ü
kendi okudu → `tap:approve` gerçekten fail-closed deny (yalnız tap:record→review); `Decision` yorumuna
`redirect` eklendi.

**Tasarım kararları (kabul):** `Context` struct (Action/Resources map anahtarı olamaz); "log" = injectable
`Set.OnAnomaly` + slog fallback (saf kalır, sinyal kaybolmaz, §4.7 yalnız sözlük); default kararı
Layer=guardrail + sid="default" (dördüncü Layer uydurulmadı). **2 bloklamayan devir:** evaluate.go:169
Türkçe yorum→M3-05/M3-08; default-Layer ayrımı→M3-07 (matched_sid ile). ŞU AN'a yazıldı.

**Sırada:** M3-05 (guardrail politikaları — §4 EN KRİTİK, iki denetçi + R8 sıra kontrolü + bounded-param
kancasını doldurma + config aralık kontrolü).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-03 done** (belge modeli + doğrulama)

**M3-03 done — üçüncü göz 1. turda ONAY.** `555e1c5`: `internal/policy/{document,validate}.go` + testler,
**%98.8 kapsam**. Belge modeli ADR 0004'e sadık (5 effect · 10 operatör · 24 anahtar · 7 eylem — denetçi
saydı, birebir). `Parse` byte-cap + strict JSON (`DisallowUnknownFields` typo'lu alanı yakalar); `Validate`
yazma-anı kapı: bilinmeyen effect/action/operatör/anahtar → **hata** (sessiz yok sayma yok — en tehlikeli
başarısızlık); ignore/redirect belgede reddedilir (yalnız kod-guardrail üretir); `sys:` rezerve
(case-insensitive, iki katman); nicel DoS sınırları (Evaluate her tap'te, tek VPS). §4.7: hata mesajı
belge değerini echo etmiyor. Saf paket — Evaluate M3-04'e.

**Denetçi non-vacuous'u 2 mutasyonla kanıtladı** (sys: kontrolü no-op → test RED; documentEffect→true →
test RED; geri alındı, sha teyit). Kendi kötü belgeleriyle 4 bilinmeyen-kategori + sys: 4 varyant + typo
alanı + §4.7 (`424242`/`SECRET`/`10.9.8.7` mesajda yok) reddini üretti; FuzzParse'ı kendi koştu.

**Devir → M3-05 (bloklamayan):** bounded-param mekanizması test edilmiş ama `DefaultLimits().BoundedParams`
BOŞ → §11 koruması üretimde fiilen yok. Denetçi düzeltmesi: **üç** eşlenebilir anahtar (gpsDistanceM,
pageAgeSeconds, **occurredAtSkewSeconds**) + debounce (config-only). M3-05 üçünü + config sınırlarını
doldurmalı (ŞU AN'a yazıldı).

**Sırada:** M3-04 (değerlendirici — saf `Evaluate`, guardrail sıralı + terminal, en-kısıtlayıcı-kazanır,
spesifik-resource-ezer, deterministik). M3-04 başında kart `Decision` yorumundaki `redirect` eksiğini düzelt.

### 2026-07-26 (4. oturum, compact sonrası) — **M3-02 done** (policy şeması, migration 00007)

**M3-02 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi ihlali yok).
`4126e4c`: migration 00007 — `policies` + `policy_versions` (**append-only**) + `policy_attachments`,
üçünde RLS beşlisi (birebir NULLIF USING+WITH CHECK). Yapıcı = agent `tappa-db-migrator`. **İki denetçi
SIRALI koşuldu** (paylaşılan Postgres'te mutasyon sondası çakışmasın diye); üçüncü göz DB'yi migration 7
temiz bıraktı, security-auditor write-sondalarını rollback tx'te yaptı.

**§4.3 kuşak+kemer non-vacuous kanıtı:** trigger DISABLE edilince superuser UPDATE **başarılı** oldu →
korumanın REVOKE (superuser atlar) değil paylaşılan `tappa_forbid_mutation` **trigger** olduğu kanıtlandı;
restore edildi. **Guardrail DB'ye yazılamıyor:** `layer` CHECK `guardrail`'i reddediyor (23514) → bir SQL
erişimi kırmızı çizgiyi kapatamaz (§4 varlık sebebi). Composite same-tenant FK çapraz-tenant link'i
blokluyor (23503); `tappa_app` rolsuper=f/rolbypassrls=f (izolasyon kökü).

**2 tasarım sapması — iki denetçi de kabul etti:** (1) `policies` DELETE **REVOKE**'lu (§4.6: silme yerine
`enabled` durum alanı; planlı silme yolu yok — seed/reset owner ile DROP kullanıyor); (2) `created_by uuid`
FK-siz (baseline'ı sistem yazar→NULL; admin FK M6/M7'ye ertelendi, M1-11 kalıbı). `policy_attachments`
tam mutable (attachment karar geçmişi taşımaz; geçmiş `transactions.policy_version_id`+`policy_context`'te).

**Ders (agent-brief'e):** paylaşılan canlı Postgres'e karşı **iki denetçi sıralı** koşulmalı, ya da write
sondaları rollback tx'inde yapılmalı — eşzamanlı RLS/trigger DISABLE + migrate down/up birbirini bozar.

**Sırada:** M3-03 (belge modeli + ayrıştırma + doğrulama — `internal/policy`, saf Go, DB yok).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-01 done · M3 başladı**

**M3-01 done — üçüncü göz 1. turda ONAY.** `01c7a8a`: `docs/adr/0004-policy-motoru-modeli.md`
(ADR 0004, policy motoru modeli). Compact noktasından temiz devralındı; ortam sağlıklı (ağaç temiz,
Go 1.26.5, tappa-db healthy). ADR, M3 kartında zaten normatif tasarlanmış modeli **gerekçeleriyle**
karara bağlar: IAM benzeri belge yapısı · 5 effect (allow|review|deny|ignore|redirect) · **2 farklı
varsayılan** (tap:*→review fail-to-review §4.6 / authz eylemleri→deny fail-closed) · 3 katman
(guardrail/baseline/tenant) · guardrail sırası NORMATİF + 2 somut sömürü (sun-invalid<employee-
deactivated → bilgi sızıntısı+push seli; sun-invalid<person-debounce → replay penceresi) · ignore/
redirect tenant'a kapalı · Y-K spesifik-kaynak-ezer · append-only sürümleme · açıklanabilirlik ·
sınırlı parametre · 4 reddedilen alternatif (if/ayar tablosu/Rego-OPA/CEL).

**Denetim (bağımsız):** üçüncü göz CLAUDE.md **§5'i satır satır** doğruladı — 1–5↔guardrail,
6–7↔baseline eşlemesi (hem tablo hem düz metin) doğru; effect eşlemesi tutarlı; operatör/anahtar/
eylem/kaynak listeleri spec ile **birebir** (10/24/7/4); biyometrik bağlam anahtarı **YOK** (§4.1);
§4 gevşetme yok; M3-05'in 10 guardrail sid'i + M3-06 baseline sid'leri kartla uyumlu; M3-03/M3-04
bu ADR'den türetilebilir. Kaçış yolu (başlık-only içi-boş) denendi, ADR düşmemiş — her madde gerekçeli.

**2 bloklamayan gözlem → M3-04'e devredildi** (ŞU AN'da): (1) M3-04 kartındaki `Decision` struct
yorumu `redirect`'i atlıyor — kart düzeltilmeli; (2) ADR eval-time bilinmeyen operatör davranışını
yazmıyor, M3-04 kartı yazıyor (çelişki yok). ADR görevi olduğu için tek genel üçüncü göz (M1-01/
M2-01 precedent'i; dual-audit kod/migration görevlerine mahsus).

**Sırada:** M3-02 (policy şeması — 3 tablo, append-only sürümler, agent `tappa-db-migrator`).

### 2026-07-26 (3. oturum, devam) — **M2-07 done · M2 KİLOMETRE TAŞI TAMAMLANDI** ✅

**M2-07 done — iki denetçi ONAY.** `cd639f5`: `sun.Verify` tüm SUN zincirini birleştirir
(parse→resolve→retired/lost→QR→unwrap+verifyMAC+Zero→**doğrula SONRA** advance) ve `Result` döner
(**verdict VERMEZ** — o M4). Vaka tablosunun tamamı + 50-goroutine tam-1 canlı `-race`; sıra kanıtı
(kötü CMAC→advance yok, last_ctr sabit); §4.7 no-leak mutasyonla; %96.5 kapsam. Sapmalar (unknown
UID→ErrUnknownTag, unwrap hatası→error) gerekçeli/kabul.

**🏁 M2 TAMAM (7/7):** RFC 4493 AES-CMAC (dış vektör) · SDM URL ayrıştırma (mixed-case kanonik) ·
session key + tek-indeksli 8-byte MAC + ConstantTimeCompare (RED yakalandı: SV2 byte-reversal
düzeltildi) · KEK sarmalama (AAD=UID, cache yok) · atomik ctr (50-goroutine tam-1 + negatif kontrol) ·
sun.Verify entegrasyonu. Tüm kripto stdlib (`crypto/aes`), yeni dep yok.

**⚠️ DEVAM EDEN GAP → M8 pilot / M4:**
- **Gerçek çip vektörü YOK:** tüm SUN zinciri self-consistent (sahte vektör) doğrulandı; RFC 4493
  CMAC dış-vektörle sabit ama SV2 ctr **mutlak** endian'ı + zincirin gerçek NTAG 424'e karşı
  doğruluğu **dış-doğrulanmadı**. **M8-05 pilot öncesi gerçek çip SUN URL'si uçtan uca test edilmeli**
  (üretim etiketleri encode edilmeden).
- **M4/M5:** `sun.Verify` `ErrUnknownTag` döner — decision/handler bunu **yutmamalı**, global güvenlik
  olayı olarak loglamalı (bilinmeyen uid'in tenant'ı yok → transactions kaydı kurulamaz; kayıt kararı M5).

**Sırada:** M3-01 (ADR 0004: policy motoru modeli).

### 2026-07-26 (3. oturum, devam) — **M2-06 done** (atomik sayaç, §4.4 en kritik)

**M2-06 done — iki denetçi ONAY** (projenin en güçlü doğrulaması). `2092796`: `sun.AdvanceCounter`
M1-08 atomik CTE'sini kullanır (verify'dan ayrı). **50-goroutine `-race` → tam 1 kazanan**; her
iki denetçi **kendi koştu** (3750+ yarış) ve **negatif kontrolü yeniden üretti** (sorguyu TOCTOU'ya
çevirince değiştirilmemiş test → 50 kazanan → harness gerçekten yarışıyor + atomiklik gerçek
koruma). tappa-security-auditor bağımsız psql sondasıyla EvalPlanQual re-fetch'i doğruladı. strict
`<`, 0-satır→ErrReplay, gömülü eşik yok (gap veri olarak döner), R4 temiz, %96.3 kapsam.

**⚠️ Devam eden gap (M2-07 + M8):** Tüm SUN zinciri şu ana kadar **self-consistent** (sahte
vektörler) doğrulandı — CMAC (RFC 4493 dış vektör ✔) hariç, SV2 ctr **mutlak** byte-sırası/endian
ve tüm zincirin GERÇEK bir NTAG 424 çipine karşı doğruluğu **henüz dış-doğrulanmadı** (skill/ADR'de
gerçek çip vektörü yok). M2-07 bunu flag'ler; **M8 pilot öncesi gerçek bir çipin SUN URL'si uçtan
uca doğrulanmalı** (üretim etiketleri encode edilmeden — M8-05 runbook).

**Sırada:** M2-07 (sun.Verify + vektör tablosu) — M2'nin son görevi.

### 2026-07-26 (3. oturum, devam) — **M2-05 done** (KEK sarmalama)

**M2-05 done — iki denetçi ONAY.** `0d23d30`: `internal/sun/keys.go` Wrap/Unwrap + Zero,
AES-256-GCM + AAD=ham 7-byte UID. **KEK parametre** (paket-seviyesi KEK state yok — cache tuzağı
kapalı); açılan anahtar uzun-ömürlü yapıya kopyalanmıyor. AAD=UID taşınabilirlik-koruması (uidA
sarıp uidB açma→hata) kendi sondasıyla kanıtlandı; **düz-anahtar/KEK sızmaz** mutasyonla (KEK
enjeksiyonu→leak testi RED); AES-256 zorlanıyor (16/24-byte KEK reddi→downgrade önlenir); 44-byte
düzen, uzunluk-KEK'ten-önce. %96.1 kapsam, redline-R7 temiz, yeni dep yok. TAPPA_TAG_KEK config'te
zaten 32-byte doğrulanıyor.

**Sırada:** M2-06 (atomik sayaç + N-goroutine eşzamanlılık — §4.4 en kritik).

### 2026-07-26 (3. oturum, devam) — **M2-04 done** (session key + truncated MAC) · RED yakalandı

**M2-04 done — iki denetçi, 2 tur.** `88c6036`: SDM doğrulama çekirdeği (SV2→K_session→boş MAC→
tek-indeksli 8-byte kısaltma→`ConstantTimeCompare`). **1. tur RED — genel üçüncü göz bloklayan
DOĞRULUK hatası buldu** (güvenlik denetçisi §4.7 merceğiyle kaçırmıştı): SV2 sayaç byte'ları
URL'ye göre **TERS**ti (M2-03 BE-parse + M2-04 LE-serialize) → palindromik-olmayan her gerçek
tap reddedilirdi, M2-07'de patlardı. **Yapısal düzeltme:** `sv2()` ham `ctrBytes`'ı verbatim
kullanır (`params.CtrBytes` eklendi); `Ctr uint32` yalnız M2-06 replay değeri için ayrı eksen.
2. tur ONAY: bağımsız Python CMAC + non-vacuous mutasyon (ctr terslenince test FAIL) ile SV2=URL
verbatim kanıtlandı; golden `d22ca9ef3a6b3b5d`. %98.9 kapsam.

**Ders:** iç-tutarlı golden byte-sırası hatasını yakalamaz; §4.7-odaklı denetçi doğruluk hatasını
görmeyebilir → bağımsız genel üçüncü göz şart oldu. **Değer-endian (M2-06 monotonik) M2-07 gerçek
vektörüne ertelendi** — reversal ekseninden ayrı.

**Sırada:** M2-05 (KEK sarmalama, Wrap/Unwrap AAD=UID).

### 2026-07-26 (3. oturum, devam) — **M2-03 done** (SDM URL ayrıştırma)

**M2-03 done — üçüncü göz ONAY.** `ac51b20`: `internal/sun/params.go`. Parse → `Params`
(UID kanonik BÜYÜK + UIDBytes ham 7 + Ctr big-endian + CMAC ham 8 + Channel/HasSUN).
**Mixed-case silent-zero-row tuzağı kapatıldı** (seed BÜYÜK saklıyor → parser uppercase kanonik;
denetçi DB sondasıyla doğruladı: `04AC…`→1 satır, `04ac…`/`04Ac…`→0). QR (ctr/cmac yok)→
sun_valid=false, hata değil; tam biri varsa hata. Big-endian ctr. §4.7 jenerik+sır-siz hata
(mutasyonla kanıtlandı). Fuzz 10.9M exec panik yok. Yeni dep yok.

**Sırada:** M2-04 (session key + tek-indeksli 8-byte MAC + ConstantTimeCompare) — kripto çekirdeği;
SV2 ctr byte sırası bilinen-cevap vektörüyle sabitlenmeli.

### 2026-07-26 (3. oturum, devam) — **M2-02 done** (AES-CMAC)

**M2-02 done — üçüncü göz ONAY.** `2380baa`: kurum-içi RFC 4493 AES-CMAC (`crypto/aes`, yeni
dep yok — ADR 0001). Dört resmi §4 vektörü PASS, K1/K2/dbl/padding testleri, **%100 kapsam**,
kısaltma yok (M2-04). Denetçi RFC vektörlerini **OpenSSL ile bağımsız yeniden hesapladı** + bayt
mutasyonuyla non-vacuous kanıtladı. API: `cmac(key, msg) ([16]byte, error)` (M2-04 kullanacak).
İki sapma (kabul): error dönüşü (§7 aes hatasını yutmaz), hata mesajı R7 "cmac" kelimesinden
kaçınacak biçimde yeniden yazıldı (daha açıklayıcı).

**Sırada:** M2-03 (SDM URL ayrıştırma).

### 2026-07-26 (3. oturum, devam) — **M2-01 done** (ADR 0003) · M2 başladı

**M2-01 done — üçüncü göz ONAY.** `5a9cd2e`: ADR 0003 (SDM modu + anahtar yönetimi). Kullanıcı
kararları: **Q05 = plain SDM** (`e81da68`), **Q06 = plaket-başına rastgele anahtar**. Normatif:
plain URL (`tag`/`ctr` big-endian/`cmac`), per-tag random AES-128, KEK AES-256-GCM
(`aes_key_ref`=nonce(12)‖ct(16)‖tag(16)=44B), MAC-input boş, ctr-wrap fail-closed, AN12196 ref.

**Denetçi bulgusu → uygulandı:** ADR AAD=UID'yi "ileri sertleştirme"ye erteliyordu; denetçi bunun
**ters** olduğunu gösterdi (pre-production, hiçbir tag sarılmadı → AAD şimdi bedava; tappa_app
`tags` UPDATE'e sahip → AAD'siz sarmalı anahtar satırlar arası taşınabilir). **AAD=ham 7-byte UID
v1'de ZORUNLU** yapıldı (Wrap(uid,key)/Unwrap(uid,ref)); aes_key_ref değişmedi.

**Sırada:** M2-02 (AES-CMAC RFC 4493, kurum-içi, dep yok).

### 2026-07-25/26 (3. oturum, devam) — **M1-11 done · M1 KİLOMETRE TAŞI TAMAMLANDI** ✅

**M1-11 done — iki denetçi ONAY** (kırmızı çizgi ihlali yok). `f416d45`: admin_users +
admin_sessions + password_resets, üçünde RLS beşlisi + REVOKE DELETE + composite same-tenant
FK. **admin'de resolver YOK** (tenant login'de bilinir — employee tap'ten farkı); admin_sessions
employee sessions'tan ayrı tablo. Q03 bcrypt (`password_hash text`, x/crypto M6-01'de). Seed
admin owner (dev-only bcrypt, round-trip doğrulandı). rls_test +3 tablo (non-vacuous: RLS DISABLE
→ RED, geri alındı). models.go make gen (deterministik). Q03 kararı: bcrypt (`8b3a0b3`).

**Denetim bulgusu (non-blocking, devredildi):** back-FK'ler (entered_by/reviewer_id/actor_id →
admin_users) M6'ya ertelendi; 00005'in "M1-11'de eklenir" yorumu artık yanıltıcı (immutable) —
ŞU AN'a M6-04/M6-01 devir maddesi + düşük notlar yazıldı.

**🏁 M1 TAMAM (11/11):** 6 migration, 11 tablo (tenants, locations, departments, employees,
sessions, tags, transactions, audit_log, transaction_reviews, admin_users, admin_sessions,
password_resets) — hepsinde RLS beşlisi; transactions/audit/reviews immutable (REVOKE+trigger);
tenant-çözümleme mekanizması (SECURITY DEFINER, GUC-anahtar denetimde reddedildi); WithTenant
(set_config, sızıntı-yok kanıtlı); ilk sqlc sorguları (make gen yeşil); RLS izolasyon+immutability
testleri (non-vacuous 3 yolla kanıtlı); KF/KM seed. Bu oturumda **M0→main merge + Q07 + Q03 +
M1-01…M1-11 + x/text CVE + redline scanner düzeltmesi** — hepsi builder→üçüncü göz (kırmızı
çizgi görevlerinde + tappa-security-auditor) döngüsünden geçti.

**Sırada:** M2-01 (ADR 0003) — **Q05 + Q06 kullanıcıya sorulacak.**

### 2026-07-25 (3. oturum, devam) — **M1-10 done** (seed) · M1 tek göreve indi

**M1-10 done — üçüncü göz ONAY.** `516be65`: `test/fixtures/seed.sql` + `ids.go`. KF 9
lokasyon + KM 5 departman, 36 çalışan, 12 tag. Bağımsız doğrulandı: idempotent (2. `make seed`
→ INSERT 0 0), 12/12 sahte-etiketli anahtar (`FAKE-WRAPPED-KEY-DO-NOT-USE-<uid>`, §4.7), yalnız
doküman IP'leri (cidr[]), Malta GPS en yakın çift 783.6 m, çift-uçlu vardiya + Rusty Bar overnight,
cross-tenant paylaşım 0, ids.go 53 UUID+12 tag DB ile birebir, yalnız master veri (transactions/
audit/reviews/sessions/admin_users hepsi 0). now()-göreli + DST-farkında Malta→UTC. Senaryo
fixtures (lost/retired plaket, invited/deactivated/null-dept/null-email çalışan).

**Sırada:** M1-11 (admin) — **M1'in son görevi.** Q03 kullanıcıya sorulacak (migration
KDF-agnostik; asıl KDF+dependency kararı M6-01). M1-11 seed'e admin owner ekleyecek + M1-09 RLS
test listesine 3 tablo.

### 2026-07-25 (3. oturum, devam) — **M1-09 done** (RLS/immutability testleri) + 2 sapma çözüldü

**M1-09 done — üçüncü göz ONAY.** `internal/db/rls_test.go` (`a033c8a`): izolasyonun ve
immutability'nin kanıtı. Üçüncü göz **non-vacuous'u 3 bağımsız yolla** doğruladı (kendi
bozup kırmızıya döndürdü, geri aldı): DB'de RLS DISABLE, trigger DISABLE, kaynak mutasyonu
`b.tenantID`→`a.tenantID`. 7 vaka + 9 tablo. M0-03 kaçış yolları kapalı (ham SQL/tenant_id
yok, pozitif kontroller, çalışma-anı `current_user`+`rolsuper/rolbypassrls` assertion'ı).
`TestStoreQueryFiltersByTenant` ayrı (izolasyon kanıtı değil). `TestResolveColumns_MatchSchema`
resolve.go drift koruması. Fixture teardown: rastgele-UUID (append-only+REVOKE DELETE teardown'ı
imkânsız kılar — imkânsızlık garantidir).

**Denetimin bulduğu 2 sapma (bloklamadı) çözüldü:**
1. **x/text CVE (`GO-2026-5970`)** — M1-07'nin pgxpool'u transitif getirmişti; `make audit`
   kırmızıydı. `go get golang.org/x/text@v0.39.0` (+x/sync v0.21.0) → govulncheck temiz,
   `make audit` yeşil. Commit `1554135`. **M1-07 denetimi bunu kaçırdı** (go build/vet/staticcheck
   CVE görmez) → agent-brief dersi: go.mod değişince `make audit`/govulncheck koş.
2. **redline R3/R5 `_test.go` yanlış-pozitifi** — RLS testi transactions UPDATE/DELETE ve
   DATABASE_MIGRATE_URL'ü meşru çalıştırıyor; yapıcı string-concat ile atlatmıştı (smell).
   Scanner düzeltildi (`--glob '!**/*_test.go'` yalnız R3 + R5-migrate-url'e; migration-beşlisi
   ve SET-LOCAL dokunulmadı), test düz literal'e döndü. **Mutasyonla dar olduğu kanıtlandı**
   (non-test .go ihlali hâlâ R3/R5 FAIL; migration ihlali hâlâ yakalanıyor). Commit `<sonraki>`.

**Sırada:** M1-10 (seed) — skill `tappa-seed`.

### 2026-07-25 (3. oturum, devam) — **M1-08 done** (ilk sqlc sorguları) · `make gen` YEŞİL

**M1-08 done — iki denetçi ONAY.** `62b70a8`: `make gen`/`build`/`dev` kırmızısı bitti.
6 tenant-kapsamlı sorgu (hepsi açık tenant_id, üretilen SQL'den okundu). `AdvanceTagCounter`
atomik CTE strict-`<` (§4.4) — canlı: 5→8 gap=2, replay→0, 2-goroutine -race tam 1 kazanan.
`GetLocationByIP` cidr[] içerme. Querier arayüzü üretildi.

**Önemli mimari bulgu:** sqlc v1.28 `SELECT ... FROM <RETURNS TABLE fonksiyonu>()`'ı
**tipleyemiyor** (ölçüldü, birkaç form denendi). → iki resolve lookup (`GetTagByUID`,
`GetEmployeeBySessionHash`) `internal/db/resolve.go`'da **elle, tipli** yazıldı; yalnız
`resolve_tag_by_uid`/`resolve_session_by_token_hash` SECURITY DEFINER fonksiyonlarını çağırır
(çıplak tablo yok), bağlamsız ham havuzda (M1-07 pool.go'nun öngördüğü dar resolver erişimi).
`resolve.sql` `-- name:`'siz kanonik-SQL belgesi olarak kaldı. ADR 0002 madde 7'ye uygulama
notu + agent-brief'e ders eklendi. Denetçiler ampirik doğruladı (bağlamsız çıplak SELECT→0,
resolver→satır — genel bypass yok).

**Q25(c):** cidr[] override **gerekmedi** (pgx/v5 varsayılan `[]netip.Prefix`, ölçüldü);
sqlc.yaml değişmedi. **WithTenant** `pgx.Tx` kaldı (RLS/resolve ham SQL ister; §7 sınırı).

**M1-09'a devredilen (bloklamayan):** store_test.go DELETE-revoked yüzünden rastgele-UUID
fixture bırakıyor → M1-09 owner-teardown ekleyebilir · resolve.go const SQL'i migration
fonksiyon imzalarıyla elle-senkron → M1-09'a sütun-sırası/tip kontrolü.

**Sırada:** M1-09 (RLS izolasyonu + değişmezlik testleri) — M0-03 3 kaçış yolu brief'e zorunlu.

### 2026-07-25 (3. oturum, devam) — **M1-07 done** (Go: havuz + WithTenant)

**M1-07 done — üçüncü göz 1. turda ONAY** (ilk Go kodu görevi). `internal/db/{pool,tenant,
tenant_test}.go` (`f73972a`): `pgxpool` sarmalayıcı (tappa_app, handler'lara açılmaz) +
`WithTenant(ctx, tenantID, fn)` — `set_config('app.tenant_id',$1,true)` param-bağlı (çıplak
SET/string concat yok), commit/rollback/panik-repanik, rollback `context.Background()` ile,
`uuid.Nil` reddi. Q27 telafisi: bağlamsız sorgu yapısal olarak imkânsız.

**Üçüncü göz üç negatif kontrolle kanıtladı** (repo-dışı kopyada): `set_config` true→false →
sızıntı testi FAIL; error-branch Commit → rollback testi FAIL; `panic(p)` silme → panik testi
FAIL. Testler vacuous değil. 5/5 -race test (aynı-backend `pg_backend_pid` sızıntı-yok kanıtı).

**İmza sapması (dokümante):** callback `func(ctx, pgx.Tx) error` — `store` M1-08'de üretilecek,
import derlenmezdi. M1-07 kartına düzeltme bloğu. Resolve erişimi M1-08'e ertelendi (havuz
açılmadı — madde 7 telafisi korundu). `go mod tidy` pgx/uuid'yi direct yaptı, **templ'i düşürdü**
(hiçbir .go import etmiyor; M2'de döner; make gen pinli @version kullandığı için etkilenmez).

**Sırada:** M1-08 (ilk sqlc sorguları) — planlı sqlc kırmızısını yeşile çevirir; resolve.sql
çözümleme sorguları + Q25(c) cidr[] override burada.

### 2026-07-25 (3. oturum, devam) — **M1-06 done** · **M1 şema katmanı TAMAM**

**M1-06 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi
ihlali yok). `00005_create_transactions_audit_reviews.sql` (`d91c609`): transactions +
audit_log + transaction_reviews, üçü append-only + RLS beşlisi.

**§4.3 immutability kuşak+kemer:** açık `REVOKE UPDATE, DELETE` (default-privilege tuzağı)
+ `BEFORE UPDATE OR DELETE` trigger — satır varken **superuser tappa_owner bile** durduruldu.
Bilinen sınır (kabul): superuser DISABLE TRIGGER / session_replication_role=replica ile
atlayabilir — bilinçli defense-in-depth, mutlak değil.

**§4.6:** nullable employee/location/department/tag_uid/ctr + CHECK'ler; çalınmış-plaket
reject, flag, manuel kayıt yazılabiliyor; **`UNIQUE(tag_uid,ctr)` yok** (reddedilen replay
kaydedilebilir). transaction_reviews 3 kısıt (UNIQUE + flag-only trigger + no-self-review).
**Çapraz-tenant review YAPISAL kapalı** — composite FK ile (denetçi trigger'ı DISABLE edip
kanıtladı: FK reddediyor, trigger değil). FLAGGED onay transactions'a dokunmuyor (Q20).

**M1 şema katmanı bitti:** 8 tablo (tenants, locations, departments, employees, sessions,
tags, transactions, audit_log, transaction_reviews) + RLS her tabloda + immutability +
çözümleme mekanizması. Kalan M1: M1-07 (Go WithTenant), M1-08 (sqlc), M1-09 (RLS testleri),
M1-10 (seed), M1-11 (admin).

**Sırada:** M1-07 — ilk Go kodu görevi. Sıralama nüansı (store.Querier henüz yok) ŞU AN'da.

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
