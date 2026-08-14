# M7 — Portal & signup

**Amaç.** Kendi kendine kayıt: pazarlama sitesi, 3 adımlı kayıt sihirbazı,
tenant provisioning. Handoff §9'daki `tappa-landing.html` ve `tappa-portal.html`
tasarımı **templ ile yeniden yazılır** (ADR 0001); tasarım dili korunur.

**Ana araç:** skill `tappa-brand`

---

## M7-01 — Landing sayfası

- **Bağımlılık:** M6-02 (bileşenler)
- **Commit:** `feat(web): add marketing landing page`

**Bölümler** (handoff §9): hero (canlı plaket + basılan adisyon animasyonu) ·
3 adım · parmak izi karşılaştırma tablosu · 4 kanıt · chain vs facility ·
fiyat (€1.50 + 3 ay ücretsiz) · FAQ · CTA → portal.

**Kabul kriterleri.**
- Slogan: *No app. No device. No fingerprints. Just tap.*
- Fontlar self-host; harici çağrı yok (GDPR).
- **Yasal sayfalar burada yayına girer (Q23):** gizlilik politikası, hizmet
  şartları, imprint/şirket künyesi, çerez bilgilendirmesi. Footer'dan erişilebilir.
  Denetimde çıktı: planda hiçbir yasal metin yoktu.
- Animasyon `prefers-reduced-motion` saygılı, sallanan/gradient efekt yok.
- Tek binary'ye gömülü (`web/embed.go`), ayrı statik host gerekmiyor.

> **Kart düzeltmesi (2026-08-13, M7-01 uygulaması sırasında).** Dört madde kart
> gerçekle çeliştiği ya da eksik kaldığı için burada kayda geçiyor.
>
> 1. **"Fontlar self-host" kriteri BU GÖREVDEN ÖNCE karşılanıyordu.** M5-04
>    `web/static/fonts/` altına altı woff2 koydu (92 KB, latin + latin-ext) ve
>    `input.css`'teki altı `@font-face` kendi origin'imize bakıyor. M7-01'in işi
>    kurmak değil **bozmamak ve ölçmek**ti: render edilen beş sayfada mutlak/
>    protokol-göreli referans **sıfır** (`TestMarketing_ReachesNoThirdParty`,
>    pozitif kontrolüyle birlikte).
> 2. **"CTA → portal" BUGÜN MÜMKÜN DEĞİL.** Sihirbaz M7-02'de; M7-01 tek başına
>    sevk edilince "Start free" düğmesinin gideceği yer yok. Sayfa bunu
>    **kelimeyle** söylüyor ve ölü bağlantı **basmıyor**;
>    `pages.LandingView.SignupHref` boş kaldığı sürece düğme render edilmiyor.
>    **M7-02'nin işi:** rotayı mount etmek **ve** `handler.Marketing.signupHref`'i
>    doldurmak — ikisi de yapılmazsa `TestMarketing_EveryInternalLinkResolves`
>    ya da `TestLanding_OffersNoLinkToAWizardThatIsNotMounted` kırmızıya döner.
> 3. **Kart `robots` meta'sından söz etmiyor ve bu bir tuzaktı.** `layout`'un ortak
>    `<head>`'i M7-01'e kadar HER sayfaya `noindex, nofollow` basıyordu — aktivasyon
>    linki bir tarayıcı botuna düşmesin diye doğru bir karar, ama bulunmak için var
>    olan bir sayfa için tam ters. Değer artık `layout.Robots` (kapalı sözcük
>    dağarcığı) ile veriliyor: landing `index, follow`, **yasal iskeletler
>    `noindex`** (yayımlanmamış bir politikanın arama sonucunda yayımlanmış gibi
>    görünmesi kabul edilemez), diğer her ekran değişmeden `noindex`.
> 4. **Yasal metinler YAZILMADI, iskele kuruldu (Q23 açık kalıyor).** Dördünün de
>    rotası, footer bağlantısı ve görünür "bu metin henüz yayımlanmadı" bloğu var;
>    her sayfa **neyi beklediğini madde madde basıyor**. Gerçek şirket künyesi,
>    veri sorumlusu, saklama süresi ve iletişim adresi bu repoda yok ve
>    uydurulmuş bir belge hukuken yanlış **ve bitmiş görünümlü** olurdu.
>    **Tek istisna çerez bilgilendirmesi:** hangi çerezlerin konduğu **kodda**, o
>    yüzden tablo çerez sabitlerinden **türetiliyor** ve
>    `TestCookieNotice_ListsExactlyTheCookiesTheProductSets` yedinci bir çerez
>    eklendiğinde kırmızıya döner.
> 5. **🔴 KIRMIZI ÇİZGİ TARAYICISI DEĞİŞTİ — [ADR 0012](../adr/0012-r1-tarayicisinin-pazarlama-muafiyeti.md).**
>    Kart bir parmak izi terminaliyle karşılaştırma tablosu istiyor, yani sayfa
>    §4.1'i **inkâr etmek için adlandırmak** zorunda. `redline-check.sh`'in R1
>    kuralına **tek ifadelik, yola sınırlı, her koşuda WARN basan** bir muafiyet
>    eklendi; ikinci ifade `activate.templ`'in kalıbı kullanılarak **gereksiz
>    kılındı**. Kabul edilen risk ve koşturulan beş sonda ADR'de.
> 6. **🔴 SAYFAYA YENİ CÜMLE EKLEYEN HERKES İÇİN — `pages.Claim` + `pages.Anchor`.**
>    2. turda denetim, sayfada **mount edilmemiş bir yetenek ilan eden** bir cümle
>    buldu: *"Reports and the monthly headcount split by department"* — iki yarısı
>    da yanlıştı (`reports.templ`'de `department` **0** kez; `ledger.Report`'ta
>    departman boyutu yok; billing yüzeyinin **beş** dosyasında da **0**). Yapısal
>    sebep cümle değil **pin yokluğuydu**: fiyat, bedava ay, çerez listesi, slogan
>    ve karşılaştırma tablosu pinliydi, bu blok değildi.
>    **Kural artık:** "iki şekil" bloğundaki her cümle bir `Anchor` **adlandırmak
>    zorunda**, ve `internal/handler`'ın `anchorDerivations` fonksiyonu her
>    anchor'ı **şemadan / üretilen sorgu tiplerinden / policy baseline'ından /
>    domain kaynağından TÜRETİR** — metin karşılaştırması **yok** (elle tutulan
>    liste bu repoda her seferinde bayatladı). Anchor'ı olmayan cümle **derlenir
>    ama testi kırar**; türetilemeyen cümle **yazılmaz**. M7-02 fiyat/VAT/kayıt
>    metni eklerken aynı disiplini sürdürmeli.
>    > 🔴 **AMA ÇAPA CÜMLEYİ DOĞRU YAPMAZ — bunu okumadan cümle ekleme.** Mekanizma
>    > yalnız **yeteneği KAYBOLAN** iddiayı yakalar; **hiç var olmamış** bir yeteneği
>    > ya da **ilgisiz bir çapaya iliştirilmiş** bir cümleyi yapısal olarak göremez.
>    > Denetim ikisini birden kanıtladı: geçen bir çapaya uydurma bir cümle
>    > (*"Tappa emails every manager a nightly summary…"*) iliştirdi, paket **yeşil**
>    > kaldı. Çapa **sürüklenmeye karşı bir cırcır**, doğruluk kanıtı değil — yeni
>    > cümleyi yine **bir insan üründe doğrulamak zorunda**. B1'de olmayan tam olarak
>    > buydu.

---

## M7-02 — Kayıt sihirbazı ve VAT

- **Bağımlılık:** M7-01 · M1-02 · Q09
- **Commit:** `feat(signup): add three-step registration wizard`

**Adımlar** (handoff §9): firma adı → VAT + işletme tipi çipleri
(Restaurant/Café/Bar/Kiosk/Retail/Hotel/Production/Other) → Single vs Multi
location + dinamik lokasyon listesi → admin hesabı → APPROVED damgalı done ekranı.

**Kabul kriterleri.**
- VAT zorunlu (gerçek işletme filtresi + fatura ön-dolumu).
- VIES davranışı Q09 kararına göre; servis kesintisi kayıt akışını **durdurmuyor**.
- `structure` (`single|multi`) seçimi lokasyon/departman modelini belirliyor.
- Sunucu tarafı doğrulama tam; istemci doğrulaması yalnızca kolaylık.
- Bot koruması (rate limit + basit challenge), CAPTCHA üçüncü parti değil.

> **Kart düzeltmesi (2026-08-13, M7-02 uygulaması sırasında).** On bir madde;
> altısı 1. turda, **6.–7. 2. turun**, **9.–11. 4. turun** bloklayan bulgularından.
>
> 1. **🔴 M7-02 / M7-03 SINIRI ÖLÇÜLDÜ VE M7-02 YAZIYOR.** İki kart aynı yazmayı
>    tarif ediyordu (bu kart *"admin hesabı → APPROVED damgalı done ekranı"*,
>    M7-03 *"Tek transaction: tenant + lokasyonlar + … + admin kullanıcı"*).
>    **Karar: M7-02 provision eder, M7-03 sertleştirir.** Eleyen ölçüm iki tane:
>    (a) **APPROVED damgası bir iddiadır** — hiçbir şey yaratmamış bir ekranda
>    basılırsa M7-01 kartının 6. maddesinin adlandırdığı *"mount edilmemiş bir
>    yetenek ilan eden cümle"* kusuru, markanın en vurgulu bileşeninde basılmış
>    olur; (b) **toplayıp devretmenin bedeli sıfır değil** — durum ya §6 beşlisini
>    dolduramayan (tenant_id **yok**, çünkü tenant henüz yaratılmadı) bir
>    `signup_drafts` tablosu, ya da zaten yazılması gereken imzalı çerez olurdu;
>    ikincisi seçildiği için M7-02'nin son adımda yazmasının **ek maliyeti yok**.
>    **M7-03'e kalan:** `multi` için departman adımı · *"plaket bekleniyor"*
>    durumu · bu turda LİMİT olarak yazılan provisioning kenar durumları.
> 2. **Q09 uygulandı ve şema gerektirdi.** `tenants`'ta `vat_verified` **yoktu**
>    → **migration 00017**. Sütun **NULLABLE** (`boolean NOT NULL` bir VIES
>    kesintisini *"bu numara geçersiz"* diye yazmak zorunda kalırdı) ve yanına
>    `vat_checked_at` kondu; dört durum: hiç sorulmadı · soruldu cevap yok ·
>    geçerli · geçersiz. VIES çağrısı **1. adımda, senkron, 3 sn timeout**;
>    başarısızlık kayıt akışını **durdurmuyor** ve `Check` **hata döndürmüyor**,
>    yani bir dal unutularak reddetmeye çevrilemiyor.
> 3. **🔴 "Basit challenge" GÖRÜNÜR BİR BULMACA DEĞİL, ve bu ölçülmüş bir
>    karardır.** Uygulanan: **honeypot alanı** + **asgari doldurma süresi (8 sn)**
>    + imzalı üç adımlı sıra + dört bütçe. Görünür aritmetik soru **reddedildi**:
>    ürünün tek dönüşüm adımına sürtünme koyar ve dört satır script'le çözülür,
>    yani gerçek müşteriye bedel, saldırgana değil. **İddia sınırlıdır:**
>    tarayıcı otomasyonu + adres havuzu olan biri geçer; bunu kapatan şey üçüncü
>    parti CAPTCHA'dır ve hem §1 hem kart onu yasaklıyor.
> 4. **🔴 KART BİLMİYORDU: BU GÖREV SEVK EDİLMİŞ BİR HESAP-KİLİTLEME AÇIĞINI
>    CANLI HÂLE GETİRİYORDU.** `internal/adminauth` `MaxCandidates = 8` ile
>    bcrypt'i sınırlıyor; `resolve_admin_by_email` ise `ORDER BY tenant_id`
>    (rastgele) sıralıyordu. **Ölçüldü:** 20 saldırgan kaydı, beş koşu →
>    **üçünde** mevcut müşteri karşılaştırılan pencerenin dışında kaldı.
>    **00017 sırayı `created_at, tenant_id` yaptı** (önce gelen önce): 5/5 koşuda
>    kurban 1. sırada, 500 satırlık sondada da 1. sırada. Gerekçe, ölçümler,
>    kabul edilen ön-kayıt varyantı ve yeni müşteri için doğan gerileme
>    **[ADR 0013](../adr/0013-kayit-sihirbazi-ve-ilk-gelen-sirasi.md)**'te.
> 5. **Sihirbaz kimseyi OTURUM AÇMIYOR** — handoff §9'un *"Go to my dashboard"*u
>    yerine done ekranı `/admin/login`'e bağlanıyor. Gerekçe: oturum vermek
>    `adminauth.Manager`'ı (token üreteci + çerez codec'i + yazma) ürünün
>    **yazan tek kimliksiz yüzeyine** takmak demekti ve `handler.Signup`'ın
>    güvenlik argümanının yarısı o alanlara **sahip olmaması**. Bedel bir tık.
> 6. **🔴 DÜZELTMENİN KENDİSİ BİR KANAL AÇTI, VE SAYILDI (2. tur, BLOKLAYAN).**
>    İlk-gelen sıralaması yerleşiği pencerenin ilk sırasına kilitliyor, dolayısıyla
>    tam `MaxCandidates` satır ekip **kendi** parolasıyla giren saldırgan seçicinin
>    satır sayısından *"bu adres kayıtlı mı?"* sorusunu cevaplıyor (**8'e karşı 7**,
>    3/3 koşu). Sıralama kanalı yaratmadı, **deterministik** yaptı — eskisi 8/9
>    olasılıkla sızıyordu. **Dört kapatma denendi ve ÖLÇÜLDÜ**, dördü de daha kötü:
>    hepsini karşılaştır (**200 aday = 45,2 sn CPU tek istekte**, 500'de ~1 dk 53 sn) ·
>    e-posta başına işletme sınırı (aynı kehanet **fiyatın üçte birine**: 3'e karşı 2) ·
>    en eskiyi ek al (doyma noktasını **bir** kaydırıyor) · rastgele örnek (yerleşik
>    **%10,1**'inde hiç karşılaştırılmıyor = **aralıklı** kilitlenme).
>    **Genel ifade:** çağıranın *doyurabildiği* her sınır sinyali sınırın kendisinde
>    yeniden üretir ve **sınırı düşürmek sondayı ucuzlatır**; kaçış yalnız sınırsız
>    pencere (DoS) ya da satır ekleyememe (**e-posta doğrulaması, taşıyıcı yok**).
>    Bedel: sondalanan **adres başına** 8 kayıt = `signupCreateLimit`'te **iki saat**.
>    Kapatılamadığı için **görünür ve sınırlı** kılındı:
>    `admin.login.candidates_truncated`, doğrulanmış tenant'a yazılıyor, hesap
>    bütçesiyle ölçülü. Tümü **[ADR 0013](../adr/0013-kayit-sihirbazi-ve-ilk-gelen-sirasi.md)**'te.
> 7. **🔴 DONE EKRANI OLMAYAN BİR PANEL YETENEĞİNİ VAAT EDİYORDU (2. tur, BLOKLAYAN).**
>    *"check the number on your dashboard"* + *"it will show as unverified on your
>    dashboard"*. Ölçüldü: `vat_verified`/`vat_checked_at` **hiçbir sorgu tarafından
>    okunmuyor**, panelde VAT ekranı/satırı **yok**, ve 00017 UPDATE grant'ini de
>    bilerek vermiyor. **M7-01'in bloklayan bulgusunun bir görev sonra tekrarı**,
>    üstelik APPROVED damgasının hemen altında — damga doğruydu, **not yanlıştı**.
>    Cümleler ürünün bugün yaptığına indirildi ve kısıt **türetilmiş** bir testle
>    tutuluyor (`TestSignupDone_PromisesNoPanelSurfaceForTheVATCheck`: üretilen sqlc
>    satır tiplerini tarar, bir ekran sütunu okumaya başlayınca kısıt kendiliğinden
>    kalkar). Paneli göstermenin iki adayı **ölçülüp reddedildi**: paylaşılan chrome
>    **her panel isteğine bir okuma** ekliyor (adminratelimit.go'nun sayfalarca
>    hesapladığı maliyet), billing ekranı M6-12'nin çitli yüzeyine altı dosya + M7-05'in
>    sonra taşıması. **Panel yüzeyi M7-05'e devredildi** (o kartta not var).
> 8. **`tenants.vat_number` GLOBAL UNIQUE olduğu için "bu VAT zaten kayıtlı"
>    cevabı veriliyor — ve bu bilinçli bir sızıntıdır.** Giriş yolu e-posta
>    varlığını asla söylemez (00011 YÜKÜMLÜLÜK 1); VAT numarası ise **VIES'te
>    herkese açık** bir kayıttır, saklamak müşteriye ne yapacağını söyleyen tek
>    cümleyi kaybettirir ve saldırgana hiçbir şey kazandırmaz.
> 9. **🔴 6. MADDENİN "GENEL İFADE"Sİ ÇÜRÜTÜLDÜ — BEŞİNCİ KAPATMA VAR (4. tur).**
>    *"Doyurulabilir her sınır sinyali yeniden üretir"* **geri çekildi**. Denetim
>    karşılaştırılan **pencereyi** değil **gösterilen listeyi** sınırladı:
>    `adminauth.PickerCap = MaxCandidates − 1` yerleşiğin işgal ettiği yuvayı tam
>    olarak soğuruyor (`min(k,C,P)` vs `min(k,C−1,P)`, `P=C−1` ile her k'de eşit).
>    k = 0…40: **kapaksız sızdıran k = [8…40], kapaklı = hiçbiri.** YÜKÜMLÜLÜK 5
>    ihlal edilmiyor — küme **daralıyor**. Bedel: 8 işletmede doğrulanan operatör
>    bir girdi kaybeder. **Dört kapatmanın aynı şekilde başarısız olması, hepsinin
>    aynı mekanizmaya nişan aldığının kanıtıydı.**
> 10. **🔴 VE ASIL KANAL SEÇİCİ DEĞİL ZAMANLAMAYDI — SONDA 8 KAYIT DEĞİL, 1 (4. tur).**
>    6. madde *"doyma altında sinyal yok"* diyordu; **yalnız sayı kanalı** için
>    doğruydu. `Authenticate` penceredeki her adayı karşılaştırıyor, **dolgu yok**:
>    tek ekili satırla, 9 çapraz tur → **216 ms'ye karşı 437 ms, %102 delta, hiç
>    örtüşme yok**. Yani ilan edilen bedel (*"8 kayıt, iki saat"*) **yanlış kanala**
>    fiyatlanmıştı ve **8× fazlaydı**. **Kapatıldı:** `adminauth.padComparisons` her
>    girişi **tam `MaxCandidates`** karşılaştırmaya dolduruyor. **Bedel: giriş başına
>    ~216 ms → ~1,75 sn** — ve **yeni DoS payı YOK**, çünkü `adminratelimit.go`
>    deneme bütçesini zaten *"10 × 8 aday × ~380 ms = ~30 sn"* üzerinden
>    boyutlandırıyor. `manager.go`'nun süresi dolan cümlesi (*"only for an address
>    that IS registered more than once"*) **adıyla emekliye ayrıldı**.
> 11. **🔴 SIRALAMA TAKASININ YARISI ÜRÜNDE YOKTU (4. tur).** 00017 ve ADR
>    *"kaydını tamamlayamayan müşteri bunu bizim sayfamızdayken öğrenir"* diyordu;
>    ölçüldü: **303 → APPROVED → "Sign in" → 401**, ve giriş ekranı açıklama
>    **yapamaz**. **Eksik yarı inşa edildi:** `Provisioner.signInBlocked` commit'ten
>    sonra adresi çözüp az önce yazdığı satırın pencerede olup olmadığını ölçüyor,
>    onay ekranı da davet etmek yerine **gerçeği** söylüyor. ⚠️ **VE BU BİT DE BİR KANAL** (6. tur):
>    *"1/2/7 hesaplı bir adres bilinmeyenle aynı cevabı verir"* yalnız saldırgan
>    **hiç satır ekmezse** doğruydu. Ölçüldü: 7 satır ek + bir kayıt daha →
>    bilinmiyorsa 8 → `false`, bir hesap varsa 9 → `true`; ekleme sayısı
>    değiştirilirse `k` **tam** öğrenilir. **Kapatılmadı, SAYILDI:** fiyat adres
>    başına **8 tamamlanmış kayıt ≈ 3 saat** (kapatılan zamanlama kanalından 8×
>    pahalı), iz `signup.sign_in_unreachable`, ve kaldırmak C1'i geri getirir.
> 12. **🔴 VE KÖK NEDEN — "sağlamadığı garantiyi beyan eden cümle" ÜÇÜNCÜ KEZ çıktı**
>    (M7-01 · B2 · C1). Artık **taranıyor**: `signupClaims` yetenek kelimelerinin her
>    birini ürüne karşı **türetiyor**, ve `TestSignupSurface_UsesNoUnanchoredCapabilityWord`
>    dokuz ekranı tarayıp sözlükte olup **çapası olmayan** her kelimeyi kırmızıya
>    çeviriyor. Komut: `go test ./internal/handler/ -run 'TestSignupSurface_' -v`.
>    ⚠️ Sınırı yazılı: yetenek **kelimelerini** denetler, **doğruluğu** değil.

---

## M7-03 — Tenant provisioning

- **Bağımlılık:** M7-02 · M1-10
- **Kırmızı çizgi:** §4.5
- **Commit:** `feat(signup): provision tenant, locations and first admin`

**Kabul kriterleri.**
- Tek transaction: tenant + lokasyonlar + (varsa) departmanlar + admin kullanıcı.
- Yeni tenant'ın verisi mevcut tenant'lardan tamamen izole (RLS testi bu akış
  için de koşuyor).
- İlk plaket kaydı için "tag bekleniyor" durumu; UID sonradan bağlanıyor.
- Başarısızlıkta yarım tenant kalmıyor (rollback).

> **Kart düzeltmesi (2026-08-13, M7-02 uygulaması sırasında).** **Bu kartın
> ilk, ikinci ve dördüncü kriteri M7-02'de KARŞILANDI** — gerekçesi ve ölçümü
> M7-02 kartının düzeltme bloğunun 1. maddesinde. Sevk edileni tekrar etmemek
> için burada ne kaldığı yazılıyor:
>
> - ✅ **Tek transaction** — `internal/domain/signup.Provisioner.Provision`:
>   tenant + lokasyonlar + ilk admin + `audit_log` satırı, hepsi bir
>   `db.WithTenant` içinde (`RecordTx`, `Record` değil).
> - ✅ **RLS izolasyonu** — `TestSignupProvision_IsInvisibleToEveryOtherTenant`,
>   **açık `tenant_id` filtresi TAŞIMAYAN** sondalarla ve pozitif kontrolüyle.
> - ✅ **Rollback** — üç test. `…ADuplicateVATLeavesNothingBehind` ve
>   `…RefusesADraftThatDidNotValidateAndWritesNothing` "yazmadı"yı sayımla değil
>   **global UNIQUE VAT numarasının hâlâ boş olmasıyla** kanıtlıyor (RLS'e bağlı bir
>   rol global sayım yapamaz).
>   🔴 **VE ÜÇÜNCÜSÜ 2. TURDA EKLENDİ, ÇÜNKÜ İLK İKİSİ YANLIŞ VAKAYI KANITLIYORDU:**
>   ikisi de **ilk ifadede** patlıyor, yani geri alınacak bir şey **hiç yazılmamış**
>   oluyor — kartın kriteri o değil.
>   `TestSignupProvision_AFailureInTheMiddleLeavesNothingBehind` transaction'ı **son
>   ifadesine kadar** götürüyor (tenant + lokasyonlar + admin yazılıyor, sonra trail
>   yazımı patlıyor) ve dördünün de geri alındığını gösteriyor — `locations` ve
>   `admin_users` dahil, ki uygulama ikisini de DELETE edemez.
> - ⬜ **DEPARTMANLAR HÂLÂ M7-03'ÜN.** Sihirbaz departman **sormuyor**;
>   `structure='multi'` onları açar ve panel ekranı M6-06'da sevk edildi.
> - ⬜ **"tag bekleniyor" HÂLÂ M7-03'ÜN.** Yeni tenant `tags` satırı olmadan
>   doğuyor — doğrusu da bu (plaketi Tappa encode edip gönderiyor, kullanıcı
>   kararı 2026-08-08) — ama bugün müşteriye bunu **söyleyen bir durum yok**.
>   Done ekranı yalnız düzyazıyla *"plaket gelene kadar dokunulacak bir şey
>   yok"* diyor; panelde karşılığı yok.
> - ⬜ **M7-02'nin LİMİT olarak devrettiği kenar durumlar** görev raporunda.

> **Faz A sevk edildi (2026-08-13) — migration 00018, ADR 0014.** M7-02'nin
> devrettiği **(b) `password_hash` format CHECK'i yok** limiti kapandı.
> `adminauth`'un *"gerekçesi süresi dolacak"* diye yazdığı dördüncü zamanlama kolu
> artık **yapısal** olarak kapalı: `admin_users.password_hash`, bcrypt'in gerçekten
> **işleyebildiği** değerlerle sınırlı.
>
> - ✅ **Ölçülen açık, ölçülen kapanış.** Migration ÖNCESİ, `tappa_app` rolüyle,
>   `BEGIN … ROLLBACK` içinde: `''` → `INSERT 0 1` · `'not-a-real-hash'` →
>   `INSERT 0 1` · **`UPDATE admin_users SET password_hash = ''` → `UPDATE 158`**.
>   SONRASI: dördü de `23514 check_violation`; işlenebilir bir digest'le pozitif
>   kontrol hâlâ `INSERT 0 1`.
> - 🔴 **KARTA DEĞİL, BRIEF'E DÜZELTME — *"yalnız biçim"* diye bir seçenek YOK.**
>   Önerilen `$2[aby]$NN$` + 60 karakter taslağı `NN`'i iki serbest haneye
>   bırakıyordu; **ölçüldü:** bcrypt yalnız **04–31** maliyetlerinde anahtar
>   programını öder, **00–03 ve 32–99** ise **0–2 µs**'de hata verir. O taslak
>   `$2a$99$cccc…`'yi kabul ederdi — 60 karakter, doğru şekil, ve **aynı 1,9 milyon
>   kat kol**. Sevk edilen kısıt maliyeti bcrypt'in kendi aralığına bağlıyor.
> - ⚖️ **Maliyet tabanı 04, ve 10 elenirken sayısı yazıldı.** `-race` altında tek
>   karşılaştırma (min-of-3, yük 4,77): cost 4 **15 ms** · cost 10 **722 ms** (48×) ·
>   cost 12 **2 837 ms** (187×); yük 7,7'deki tek atışlarda oran **67×/310×** —
>   yani bant **48–67×**. **Şıkkı eleyen sayı oran değil, uçtan uca duvar saati:**
>   `internal/adminauth` fixture'ları cost 10'a alınınca paket
>   **138,6 s → 295,1 s** (+156,4 s, 2,13×) — ve bu tek paket.
>   210× cost-4 kolu bu yüzden **disiplinsel** kalıyor (`adminauth.Cost = 12` +
>   signup'ın *saklanan satırı geri okuyan* `$2a$12$` testi). Gerekçe: **ADR 0014**.
> - 🔴 **VE BİR TAVAN VAR: 14.** 2. turda eklendi. Kısıt önce bcrypt'in maksimumunda
>   (31) bitiyordu; bu **altta doğru, üstte yanlıştı**. `manager.go:475` dolguyu
>   **ilk adayın digest'inden** fiyatlıyor ve döngü **her** adayı karşılaştırıyor,
>   yani **tek** yavaş satır o e-postanın bütün girişlerini durduruyor — başka bir
>   tenant'taki meşru sahibininki dahil. **Referans ölçüm** (min-of-3, `-race` yok,
>   yük **5,54**): cost 12 **214 ms** · cost 14 **892 ms** (8 adayda **7,1 s**) ·
>   cost 31 **~31 SAAT** (8 adayda ~249 saat). ⚠️ Cost 12 yüke bağlı, dört bağımsız
>   okuma **210–312 ms**; yavaş uç taban alınırsa cost 14 ~1,25 s / 8 adayda ~10 s —
>   **karar bandın her yerinde aynı, yavaş uçta güçleniyor**. Tavan 14 = ürünün hâlâ
>   çalıştığı en yüksek maliyet, `Cost = 12`'nin üstünde iki katlama payı. Bugünkü
>   bedeli **sıfır** (repo yalnız 4 ve 12 üretiyor).
> - ⚠️ **`NOT VALID`** — **20 232 / 37 873** satır ihlal ediyor (2026-08-13; sayı
>   her koşuda büyür, 00013'ün kaydettiği aynı uyarı). **Ürün verisi temiz:** seed'in
>   admin satırı **2/2** `$2a$12$`. ⚠️ state.md'nin *"seed'in **140/140** satırı"*
>   ifadesi **yanlış** — seed `admin_users`'a **iki** satır yazar.
>   ✅ Donma **geri alınabilir** (ölçüldü): donmuş satıra yasal digest yazan UPDATE
>   başarılı → çaresi **parola sıfırlaması**. ⚠️ `VALIDATE`'i **hiçbir şey
>   koşturmuyor**; boş tabloda bile `convalidated = f`.
> - ⬜ **Faz B'ye kalan:** departman adımı · *"plaket bekleniyor"* durumu.

---

## M7-04 — Admin daveti, şifre sıfırlama, e-posta

- **Bağımlılık:** M7-03 · Q02
- **Kırmızı çizgi:** §4.7
- **Commit:** `feat(auth): add admin invite and password reset`

**Kabul kriterleri.**
- Magic link ve/veya şifre ile giriş (handoff §9).
- Sıfırlama token'ı tek kullanımlık, süreli, hash'i saklanıyor.
- E-posta sağlayıcısı AB bölgesinde, GDPR işleme sözleşmesi var (Q02).
- Gönderim başarısızlığı kullanıcıya dürüstçe bildiriliyor, sessiz yutulmuyor.
- Token/kod log'a **yazılmıyor**.

---

## M7-05 — Hesap ve marka mesajı ayarları

- **Bağımlılık:** M7-03 · M6-01
- **Commit:** `feat(dashboard): add account settings`

**Kabul kriterleri.**
- Firma bilgileri, fatura verileri, plan görünümü.
- Zaman dilimi ayarı (Q01 kararına göre tenant/lokasyon seviyesinde).
- Marka mesajları görünür (düzenleme editörü M9-04); varsayılanlar KF/KM
  metinleri.
- Her değişiklik `audit_log`'a yazılıyor.

> **Kart eklemesi (2026-08-13, M7-02 uygulaması sırasında).** **VAT DOĞRULAMA
> DURUMU BU KARTIN İŞİ.** M7-02 `tenants.vat_verified` + `vat_checked_at`
> sütunlarını yazıyor (migration 00017, dört durum: hiç sorulmadı · soruldu cevap
> yok · geçerli · geçersiz) ama **hiçbir panel ekranı okumuyor** — kayıt
> sihirbazının onay ekranı müşteriye bunu söyleyen **tek** yer ve o ekran bir kez
> gösterilip siliniyor. Yani bugün VIES'in **reddettiği** bir numara veritabanında
> `false` olarak duruyor ve müşteri onu bir daha hiçbir yerde göremiyor.
> **Bu kart açarken üç şey birlikte gelmeli:** sütunları okuyan bir sorgu · "firma
> bilgileri" ekranında görünen bir satır · ve **00017'de bilerek verilmemiş olan
> `UPDATE (vat_verified, vat_checked_at)` grant'i** (yeniden kontrol için — kimin,
> ne sıklıkla tetikleyebileceği bu kartın kararı, çünkü müşterinin istediği zaman
> tetiklediği bir dış çağrı farklı bir tehdit modelidir).
> ⚠️ Onay ekranının metni `TestSignupDone_PromisesNoPanelSurfaceForTheVATCheck` ile
> ürüne bağlı: bir sorgu sütunu okumaya başladığı gün test kısıtı **kendiliğinden**
> kaldırıyor, yani cümleyi geri yazmak serbest kalıyor.
