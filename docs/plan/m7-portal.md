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

> **Faz B sevk edildi (2026-08-14) — yeni migration YOK, yeni ADR YOK.** Kartın son
> iki ⬜ maddesi kapandı, ama **ikisi de kartın tarif ettiği şekilde değil** —
> ikisinin de gerekçesi ölçümle değişti. Yeni sorgu **iki** tane
> (`CountTenantPlaques`, `TenantHasAnyTransaction`), yeni sütun/tablo yok.
>
> 1. **🔴 DEPARTMAN ADIMI EKLENMEDİ, VE ELEYEN ÖLÇÜM KARTIN KENDİ CÜMLESİNİ
>    ÇÜRÜTTÜ.** Kart *"`structure='multi'` onları açar"* diyor; `internal/domain/
>    signup/signup.go` de aynı şeyi yazıyordu (*"`multi` is several venues, and
>    departments under them"*). **Üç ölçüm bunun tersini söyledi:**
>    - **Ürünün kendi demo verisi ters yönde.** Kebab Factory `multi`, dokuz
>      lokasyon, **0 departman**; Kebab Manufacturing `single`, tek lokasyon,
>      **5 departman** ve **beşi de kendi vardiyasını taşıyor** (04:00/05:00/06:00/
>      08:00/09:00). Yani departmanı gerçekten kullanan tek tenant `single`.
>      Komut: `docker compose exec -T db psql -U tappa_owner -d tappa -c "SELECT
>      t.name, t.structure, count(d.*) FILTER (WHERE d.id IS NOT NULL) AS deps,
>      count(d.shift_start) AS own_shift FROM tenants t LEFT JOIN departments d ON
>      d.tenant_id=t.id WHERE t.id IN ('10000000-0000-4000-8000-000000000001',
>      '20000000-0000-4000-8000-000000000001') GROUP BY 1,2;"`
>    - **`tenants.structure` yazıldıktan sonra HİÇBİR ŞEY okumuyor.** `CreateTenant`
>      sütunu adlandıran tek ifade; hiçbir ekran, sorgu ya da policy ona dallanmıyor.
>      `TestSignupStructure_DecidesNothingAfterSignUp` (go/ast, iki pozitif kontrol)
>      bunu tutuyor — bir gün gerçekten dallanırsa test kırmızıya döner ve sihirbazın
>      metni **o anda** yeniden ele alınır.
>    - **Panel iki şekle de aynı kontrolü veriyor.** Sihirbazdan uçtan uca iki tenant
>      provision edildi (`single` ve `multi`); `/admin/locations` ikisinde de
>      *"Add a department"* düğmesini ve *"This business does not use departments,
>      which is perfectly normal…"* boş durumunu basıyor. Departman ekranı iniş
>      ekranından **1 tık** uzakta (tab çubuğu → Locations & Wall Tags).
>
>    **Yani sorulan soru (*"`multi` seçen departmanların varlığını nasıl öğreniyor?"*)
>    yanlış yarıya bakıyordu: `multi` zaten öğreniyordu** — adım 1'deki kart
>    *"You can add departments under them later"* diyordu. **Öğrenmeyen `single`'dı**,
>    ve ürünün elindeki tek veride departmanı kullanan şekil o. Sevk edilen düzeltme
>    bir **adım değil bir cümle**: departman cümlesi karttan çıkarılıp fieldset'in
>    altına, **iki şekle birden** ait olacak şekilde taşındı. Sihirbazın adım sayısı
>    **3** kaldı; **kullanıcının doldurduğu** alan sayıları **4 / 1
>    (tekrarlanabilir) / 3**. Sayılmayan: her adımdaki `csrf` gizli alanı ve adım
>    3'teki honeypot (`internal_ref`).
>    ⚠️ **HAM SAYIM YAZILMADI, çünkü hiçbir tek kuralla üretilemiyordu.** İlk hali
>    *"5/1/4"* diyordu; csrf+honeypot dahil edilirse **5/2/5**, yalnız düz `name="`
>    aranırsa (honeypot `name={ v.HoneypotField }` yazıldığı için kaçar) **5/2/4** —
>    ikinci adım her iki okumada da **2**. *"Neyin sayılmadığını"* söylemesi gereken
>    bir cümlenin içinde **üçüncü, yazılmamış bir kural** vardı. Ham sayıya ihtiyaç
>    olursa okuma kuralı da yazılmalı.
>
>    ⚠️ **SINIR: n = 1'e karşı n = 1.** *"`single` departmanı daha çok kullanır"*
>    İDDİA EDİLMEDİ ve edilemez — elde iki seed tenant var. İddia edilen dar olan:
>    **kuplaj yok** (şema, sorgu ve panel üçü de bunu söylüyor) ve **cümle kuplaj
>    varmış gibi yazılmıştı**.
>
> 2. **🔴 "TAG BEKLENİYOR" — KARTIN *"panelde karşılığı yok"* CÜMLESİ YANLIŞTI, AMA
>    BOŞLUK BAŞKA YERDEYDİ.** `locations.templ:246` zaten basıyor ve buton
>    koymamayı gerekçesiyle yazıyor (kullanıcı kararı, 2026-08-08). **Gerçek boşluk
>    İLK İNİLEN EKRANDI ve uçtan uca sürülerek bulundu:** sihirbazdan çıkan tenant
>    *"Sign in to your dashboard"* düğmesiyle `/admin`'e (Transactions) iniyor ve
>    orada şunu okuyordu — *"Nothing was recorded here on this day. **Pick another
>    day above.**"* Kırk saniyelik bir işletme tarih seçiciye yollanıyordu.
>
>    Sevk edilen: `/admin`'in başına, **işletme hakkında** (gün hakkında değil) bir
>    `Notice`. Üç durum, üç cümle, hepsi **`tags` üzerindeki üç sayımdan türetiliyor**
>    (`CountTenantPlaques`: `in_service` / `in_stock` / `loaded`). `in_service`
>    yüklemi **tap yolunun kendi yüklemi** (`status='active'`, §5 satır 1) — ekran
>    reddedecek koşulun ikinci bir kopyasını değil, koşulun kendisini okuyor.
>    `in_stock` **çıkarmayla türetilmiyor**: kalan `retired`+`lost`'tur, ve son
>    plaketi emekli olmuş bir işletmeye *"kutuda bekliyor"* demek onu boş bir çekmeceye
>    yollardı (`TestPlaqueState_IsNotInferredFromASubtraction`).
>
>    ⚠️ **YAZILMASI YASAKLANAN CÜMLE: *"plaketiniz yolda."*** Şemada sipariş, sevkiyat
>    ya da tarih **yok**; bu cümle müşterinin bize güvenip güvenmeyeceğine karar
>    verdiği anda uydurma olurdu. `TestPanelLanding_MakesNoDeliveryClaim` on iki
>    **regex** kalıbını (kendi pozitif kontrolüyle) sayfanın **görünür metninde**
>    tarıyor.
>    ⚠️ **VE İKİNCİ BİR CÜMLE TASLAKTA ÖLDÜ:** *"bu sayfaya hiçbir şey düşemez"*
>    **yanlış** — müdürün elle yazdığı kayıt plaket istemez, ve kutudaki/emekli
>    plakete yapılan tap **reddedilir ama yine de yazılır** (§4.6). İzin verilen en
>    güçlü iddia *"nobody can tap in or out"*; ADR 0006 yazılan satırın dokunuş
>    olmadığını zaten söylüyor. `TestPanelLanding_ClaimsOnlyThatNOBODYCANTAP`.
>
>    🔴 **VE AYNI CÜMLE ÜRÜNÜN KENDİ SİHİRBAZINDA SATILIYORDU (2. tur bulgusu).**
>    `signup.templ` onay ekranı *"Tappa encodes them and **posts them to you**"*
>    diyordu — panelde yasaklanan iddianın ta kendisi, bir tık ötede. Kalıp listesi
>    düz metin (*"posted to you"*) olduğu için **çekim farkı** (`posts`) içinden
>    geçiyordu ve o sayfa zaten taranmıyordu. Üçü birden düzeltildi: cümle
>    `locations.templ`'in *"loads it here"* biçimine indirildi, kalıplar **regex**
>    oldu, ve `TestSignupSurface_MakesNoDeliveryClaim` aynı kalıpları **dokuz
>    sihirbaz ekranına** çeviriyor.
>
>    🔴 ***"Pick another day above"* GERİ ÇEKMESİ 1. TURDA YANLIŞ OLGUYA BAĞLIYDI
>    (2. tur, denetçi gerçek `INSERT`'le kanıtladı).** Geri çekme *"hiç plaket
>    yüklenmemiş"*e bağlıydı — **oysa manuel kayıt plaket İSTEMEZ**: sıfır plaketli
>    bir tenant'a `channel='manual'`, `tag_uid` NULL bir satır yazılabiliyor. Yani
>    tavsiye, **kayıtları gerçekten başka günde olan tek kişiden** alınıyordu.
>    Artık **`TenantHasAnyTransaction`** olgusuna bağlı: kaydı olan işletme her
>    zaman tarih seçiciye yönlendirilir, olmayandan tavsiye çekilir, **ölçülmemişse
>    tavsiye KALIR**. Regresyon: `TestPanelLandingDB_ManualRecordsKeepTheDatePickerOffered`.
>
> 3. **⚠️ MALİYET, VE NEREYE KONMADIĞI.** İki okuma da `ledger.Screen`'in **zaten
>    açık olan** transaction'ına biniyor: **ek bağlantı ve ek transaction yok — ama
>    ek İFADE var, ve bir ifade bir gidiş-dönüştür.** Ölçüldü (`EXPLAIN ANALYZE
>    BUFFERS`, 5 koşu, yük 2,6–4,1; `tags` 40.544 satır/34.545 tenant, `transactions`
>    128.067 satır/25.954 tenant):
>    - `CountTenantPlaques` **indeks-only DEĞİL** — `status` indekste yok →
>      `Bitmap Heap Scan`, **Heap Blocks: exact=5**, `shared hit=8`,
>      **0,209/0,217/0,232/0,358/1,433 ms** (15 plaketli KF tenant'ı).
>    - `TenantHasAnyTransaction` **Index Only Scan** — `shared hit=4` (10.193
>      satırlı KF) / `3` (kaydı olmayan tenant), **0,063–0,203 ms**. İlk satırda
>      durduğu için **tenant büyüklüğüyle büyümüyor**.
>      ⚠️ **`Heap Fetches: 0` bir GÖZLEMDİR, özellik değil — VACUUM'a bağlı.**
>      Aynı gün iki yönde de ölçüldü: oturmuş KF tenant'ında **0** (`hit=4`), saniyeler
>      önce yazılmış bir tenant'ta (`BEGIN … ROLLBACK`) **1** (`hit=5`). Kalıcı olan
>      **plan düğümü** ve **sınır**: EXISTS ilk satırda durduğu için vacuum durumundan
>      bağımsız olarak **en fazla bir heap sayfası** — plaket sayımının beş bloğuna
>      karşı asıl fark bu.
>
>    **Paylaşılan chrome'a KONMADI** — M7-02'nin 7. maddesinin ölçtüğü bedel bu
>    (her panel isteğine bir okuma × sekiz bölüm); müşteri **Transactions**'a iniyor,
>    okuma da orada.
>
> 3b. **🔴 `make audit`'İN İKİNCİ TARAMASI HİÇ KOŞMUYORDU (2. tur, kapsam dışı ama
>    bilinçli).** `audit:` iki ayrı recipe satırıydı; govulncheck sıfırdan farklı
>    dönünce make **duruyor** ve `./scripts/redline-check.sh` **hiç çalışmıyordu**.
>    Ölçüldü — **ÖNCE:** `make audit` exit 2, çıktının tamamında *"redline"/"mekanik
>    tarama"* **0 eşleşme**. **SONRA:** aynı exit, **4 eşleşme** ve son satır
>    `audit: govulncheck exit=1 - redline-check exit=0`. Tehlikeli yapan şey T31'in
>    **bilinen kırmızı** olması: *"make audit zaten kırmızı"* alışkanlığı, ikinci
>    güvenlik ağının sessizce kapalı olduğunu gizliyordu. Tek shell, iki çıkış da
>    basılıyor, ikisi de sıfır değilse hedef başarısız.
>
> 4. **⬜ KAPATILMAYAN, FİYATIYLA:** `/admin/review`'un boş durumu hâlâ *"Every
>    flagged tap has been decided."* diyor — hiç flag almamış bir tenant için boşuna
>    doğru, ama **yapılmamış bir işi yapılmış gibi** okutuyor. `/admin/reports` da
>    *"Pick another week above"* diyor — ve artık **`TenantHasAnyTransaction` bu
>    cümleyi de düzeltebilecek olguyu taşıyor**, yani fiyat sorgu yazmak değil o
>    bölüme bir ifade daha eklemek. **Fiyatı: bölüm başına bir ifade** (ya da
>    chrome'a taşınırsa sekiz bölüm × bir ifade).
>
> 5. **⚠️ 2. TURDA DÜZELTİLEN ÜÇ CÜMLE — hepsi "ölçmeden kopyalama"nın izi.**
>    (a) *"ONE SNAPSHOT"* — `WithTenant` `pool.Begin` ile açıyor, izolasyon **READ
>    COMMITTED**, **her ifade kendi snapshot'ını alır**; denetçi tek transaction
>    içinde iki özdeş sayımın 0→1 döndüğünü ölçtü. Cümle *"aynı transaction ve aynı
>    tenant bağlamı"*na indirildi (`REPEATABLE READ`'in bedeli var, alınmadı). Bu
>    iddia **option sorguları için önceden de** yazılıydı ve kopyalanmıştı — ikisi
>    de düzeltildi.
>    (b) *"three counts over tags_tenant_idx"* — **indeks-only ima ediyordu, değil**
>    (yukarıdaki ölçüm). Aynı dosyanın `ListTagLastSeen` için taşıdığı düzeltmenin
>    tekrarıydı.
>    (c) `PlaqueStateWorking`'in *"taps can be recorded"* gerekçesi — **sayımdan
>    türetilemez**: duvarda plaketi olan ama aktive olmuş çalışanı olmayan tenant
>    §5 satır 3'e düşer ve **hiç kayıt yazılmaz**. Ekran o durumda zaten susuyordu;
>    yanlış olan yalnız gerekçeydi.
>
> 6. **⚠️ SİLİNEN ÖLÜ KOD:** `TransactionsView.NoTapCanBeRecorded()` — hiçbir şablon
>    çağırmıyordu, yalnız kendi testi için vardı (`PlaqueState()`'in ikinci bir
>    temsili). Silindi; yerinde yalnız mezar taşı yorumu var.
>
> 7. **🔴 3. TUR — TESLİMAT TARAYICISI, YERİNE GEÇTİĞİ CÜMLEYİ KAÇIRIYORDU; VE ASIL
>    KUSUR POZİTİF KONTROLDEYDİ.** Denetçi 12 regex'i sekiz bilinen-kötü cümleye
>    uyguladı: **altısı geçti**, aralarında **kaldırdığım literalin edilgen hâli**
>    (*"Your plaques will be posted to you next week"*). Sebep: bir desen **nesne
>    zamirini zorunlu kılıyordu**, edilgen *"posted TO you"* vermiyor.
>    **Asıl ders:** `assertDeliveryScannerWorks` örnek cümleyi **desenlerden**
>    üretip *"her desen eşleşti mi"* diye soruyordu — **inşa gereği doğru** bir soru.
>    Bu yüzden **delik açıkken yeşil kaldı.** Bu projenin **beşinci** vacuous testi
>    ve şekli yeni: **kendi kendini doğrulayan pozitif kontrol.**
>    **Sevk edilen:** (a) 16 desen, fiil + yakındaki alıcı kalıbıyla (`send/sent`,
>    `mail`, `en route`, `expect …`, edilgen `post…to you`); (b) **kontrol ters
>    çevrildi** — desenlerden cümle üretilmiyor, **18 bilinen-kötü cümlelik sabit bir
>    corpus** (ilk sekizi denetçinin, kelimesi kelimesine) tutuluyor ve **her birinin
>    en az bir desene takıldığı** iddia ediliyor; (c) **negatif kontrol** eklendi —
>    ürünün gerçekten sevk ettiği dokuz cümlenin **hiçbiri** eşleşmemeli, yoksa
>    tarayıcı bir tripwire değil **düzyazı yasağı** olur.
>    **Mutasyon:** eski desen geri konunca corpus testi **denetçinin iki cümlesini
>    adıyla** kırmızıya döndürüyor; tek desen silinince de; bir desen genişletilince
>    negatif kontrol düşüyor.
>
>    🔴 **4. TUR: DESENLER HÂLÂ ZAMİR DAYATIYORDU.** Yeni bir denetim 20 bilinen-kötü
>    cümle yazdı, **16'sı kaçtı** — ikisi doğrudan bu deliğin devamı: *"We are posting
>    **your plaques** tomorrow"*, *"We will send **the plaques** on Monday"*. Yorum
>    *"fiil + yakındaki alıcı"* diyordu; alıcı listesi hâlâ **yalnız zamirdi**, isim
>    nesnesi geçiyordu. Ve 3. turun *"genişletmek negatif kontrolü düşürür"*
>    savunması **yalnız çıplak `plaques?` için** doğruymuş: `(the|your|our|their)
>    plaques?` ile **çıpalanınca** iki kaçak da yakalanıyor ve negatif kontrol
>    **9/9 temiz** kalıyor. **Sevk edilen:** 20 desen (kurye adları, `carrier`,
>    `consignment`, `parcel`, `packed`, `warehouse`/`depot`, `despatch`,
>    `should be with you`, `has been sent`), corpus **30 cümle** — ölçüm:
>    **30/30 yakalandı, 0 yanlış pozitif.** Mutasyon: zamir-listesi geri konunca
>    corpus testi *"We are posting your plaques tomorrow."* diye kırmızıya dönüyor.
>
>    🔴 **5. TUR: ÇIPA ÜRÜNÜN KENDİ İKİNCİ ADINI KAÇIRIYORDU.** Çıpa
>    `(the|your|our|their) plaques?` idi; oysa ürün aynı şeye **"Wall Tags"** diyor
>    (panel bölümünün adı `PanelSections`'ta *"Locations & Wall Tags"*) ve **bu
>    ifade negatif kontrolde birebir duruyor**. Yani isim-nesnesi deliği ürünün
>    **kendi kelimesiyle** hâlâ açıktı: *"We are printing and posting **the wall
>    tags** this week."* geçiyordu. Çıpa `(plaques?|wall tags?|tags?)`'e genişletildi;
>    ayrıca `en[- ]route` (tire), `(leaves?|left|leaving)`, `tracking\s*:`.
>    **Ölçüm: 35 cümlelik corpus → 35/35 yakalandı; negatif kontrol 9/9 temiz.**
>    **Mutasyon:** `wall tags` çıpası kaldırılınca corpus testi o cümleyi **adıyla**
>    kırmızıya döndürüyor (hem panel hem sihirbaz tarafında).
>
>    ⚠️ **KAPSAM — KAPATILMADI, SAYILDI.** Desenler **çıpalı**; çıpasız teslimat
>    iddiaları geçmeye devam eder: *"Your kit is with the postman."*, *"Yours goes
>    out tomorrow."*, *"Allow 3-5 days."*, *"It should turn up in a few days."*,
>    *"They're already in the mailbag."* Çıpasız desenler (`\bout\b`, `\bdays?\b`,
>    `\bpost\w*\b`) negatif kontrolü düşürür — **ölçüldü**. Fiyat: ya yanlış pozitif,
>    ya bu sınıf. Sınıf seçildi.
>
> 8. **🔴 3. TUR — `redline-check.sh` `rg` YOKKEN YEŞİL GEÇİYORDU.**
>    `env PATH=/usr/bin:/bin ./scripts/redline-check.sh` → *"tarama atlaniyor"* +
>    **EXIT=0**; `make audit` (govulncheck yeşil sahte) → **exit 0** ve
>    *"redline-check exit=0"*. Yani **koşmayan** tarama **temiz** taramadan ayırt
>    edilemiyordu — üstelik 3b'de başlığına *"iki tarama da koşar"* yazdığım hedefte.
>    Delik diff'imden eski ve **CI'da kapalı** (`ci.yml` rg'yi kurup `rg --version`
>    ile kanıtlıyor) — kör olan yerel döngüydü. **Sevk edilen:** script artık
>    **exit 2** (1 değil: 1 *"ihlal bulundu"*, 2 *"araç eksik"* — çağıran ucunu
>    ayırt edebilsin) ve `make audit` özeti ayrı kelimeyle basıyor:
>    `redline-check SKIPPED(no rg) exit=2`. **2×2 ölçüldü:** rg var + govulncheck
>    yeşil → **exit 0**; rg yok → **exit 2**.
>
>    🔴 **4. TUR: AYRIMIN YARISI EKSİKTİ — BOZUK `rg` HÂLÂ "TEMİZ" GÖRÜNÜYORDU.**
>    `command -v` yalnız *"dosya var mı"* diye sorar; bozuk bir `rg` shim'inde
>    (exit 2) ve `chmod -x` edilmiş bir dosyada (exit 126) **başarılı** döner, ve
>    `scan()` rg'nin stderr'ini `2>/dev/null` ile yutup çıkış kodunu hiç okumaz →
>    *"✓ mekanik tarama temiz"* + **EXIT 0**. Yani ayrım yalnız *"rg yok"* için
>    vardı, *"rg çalışmıyor"* için yoktu. **Sevk edilen tek satır:**
>    `have_rg() { command -v rg … && rg --version …; }`. **Beş yol ölçüldü:**
>    yok → **2** · bozuk → **2** · çalıştırılamaz → **2** · sağlam+temiz → **0** ·
>    sağlam+ihlal → **1** (ikisi karışmıyor). Mutasyon: `command -v`'ye dönülünce
>    bozuk rg yine *"✓ mekanik tarama temiz" exit=0*.
>
>    🔴 **5. TUR: SINIF KAPANMAMIŞTI, DARALMIŞTI — `scan()` rg'nin ÇIKIŞ KODUNU HİÇ
>    OKUMUYORDU.** Denetçi **gerçek rg** ile yendi: geçersiz bayrak içeren
>    `RIPGREP_CONFIG_PATH` altında `rg --version` **exit 0** (yapılandırma
>    okunmuyor) ama her arama **exit 2**; script *"✓ mekanik tarama temiz"* + exit 0
>    basıyordu — **ağaçta gerçek bir R7 ihlali dururken**. rg'de `0`=eşleşme var,
>    `1`=eşleşme yok, `≥2`=hata. **İki bağımsız koruma kondu:** (a) `have_rg` artık
>    `--version` değil **gerçek bir tarama** koşuyor; (b) `scan()` çıkış kodunu
>    okuyup `≥2`'de bir işaretçi dosyasına yazıyor, script sonda onu okuyup
>    **exit 2** veriyor — `exit` doğrudan çalışmaz, çünkü `scan` daima `$(…)` içinde,
>    yani **alt kabukta** koşuyor (ölçüldü). **Yedi yol:** yok·bozuk·çalıştırılamaz·
>    **tarama patlıyor** → **2** · temiz → **0** · gerçek ihlal → **1** · tarama
>    patlıyor **ihlal varken** → **2**. **Mutasyon:** korumaların biri kaldırılınca
>    diğeri hâlâ **2** veriyor; **ikisi de** kaldırılınca (4. turun hâli) ihlal
>    varken *"temiz" + exit 0*.
>
> 9. **🔴 3. TUR — `TenantHasAnyTransaction`'ın çalışma-zamanı kemeri eklendi.**
>    Kardeşi `CountTenantPlaques` aynı fazda almıştı, bu almamıştı — denetçi
>    **üretilmiş** ifadeyi bilerek mutasyona uğratıp (çünkü ölçtüğü şey **üreten ağ
>    değil çalışma-zamanı ağı**) tam suite'i koştu: **tek test kırmızı olmadı.**
>    `TestBelt_Tags00013_…/TenantHasAnyTransaction` eklendi (A'nın bağlamı + B'nin
>    id'si → `false`; A'nın id'si → `true`). **Mutasyonla doğrulandı.**
>
> 10. **⚠️ 3. TUR — İKİ TAVSİYE OKUMASI ARTIK SAYFAYI DÜŞÜRMÜYOR.** `Plaques.Queried`
>    ve `History.Queried` tam da *"ölçmedik"* hâli için yazılmıştı, **ama tek üretici
>    ya `true` yazıyor ya sayfayı 500'e düşürüyordu** — yani bayrakların tasarlandığı
>    bozulma yolu **ulaşılamazdı**. §4.6 ihlali değildi ama **kullanılabilirlik
>    bağlantısıydı**: bir *tavsiye* sayımının hatası, bir ifade önce **başarıyla
>    okunmuş** delil sayfasını müdürden alıyordu. Yeni `Reader.advisories` **hata
>    döndürmüyor**: her iki hata **ERROR olarak loglanıyor** (§7 — yutulan yok),
>    okuma `Queried=false` kalıyor, ekran susuyor / tavsiye kalıyor. İki okuma
>    transaction'ın **son iki ifadesi**, yani buradaki bir hata kayıtları kaybetmiş
>    bir hata olamaz (ulaşılamaz veritabanı **`pool.Begin`'de**, tek bir ifade
>    koşmadan patlar → doğru 500).
>
>    🔴 **VE 4. TUR BUNU BLOKLAYAN OLARAK REDDETTİ: KADEMELENDİRME ÜRETİMDE
>    ETKİSİZDİ.** Go hatasını yutmak yetmiyor — **Postgres transaction'ı ifade
>    hatasında ABORT eder**, dolayısıyla `db.WithTenant`'ın `tx.Commit()`'i
>    `pgx.ErrTxCommitRollback` veriyor → `Screen` hata → **handler yine 500**.
>    Yorumun saydığı üç sebebin (yetki, zaman aşımı, sayımdaki hata) **üçü de**
>    ifade hatasıdır. Ve testin bunu görememesinin sebebi **3. turda kendi
>    bildirdiğim kusurun tekrarıydı**: test `Screen`'i hiç çağırmıyor, callback'ten
>    sentinel dönüp **rollback** ediyordu — yani **commit yolu hiç koşmuyordu**.
>    **Sevk edilen (i): her advisory okuması kendi `SAVEPOINT`'inde** (`pgx.Tx.Begin`
>    → SAVEPOINT; hatada `ROLLBACK TO`, başarıda `RELEASE`). **Fiyat: istek başına
>    dört ekstra ifade**, zaten açık bağlantıda, disk/WAL yok.
>    **Elenen iki şık:** *(ii) ayrı transaction* — **ikinci bağlantı** demek, backlog
>    **T34**'ün sınıfı ve önceki denetimin `pool_max_conns=1` kanıtını çökertirdi;
>    *(iii) geri al* — sayılmış bir sınır dürüst olurdu ama iki savepoint bundan
>    ucuz. **İki savepoint tek yerine ayrı:** `tags` kilitlenirse `transactions`'tan
>    okunan tarih cevabı da kaybolurdu.
>    Kanıt artık **`Screen` üzerinden, commit dahil**:
>    `TestAdvisoriesDB_AFailedAdviceCostsTheSentenceAndNeverTheRecords` — oturuma
>    özel bir `TEMP TABLE tags` gerçek tabloyu gölgeliyor (42703; **tablo kilidi
>    değil** — kilit paralel koşan diğer paketleri de bloke ederdi), `Screen`
>    **nil** dönüyor, kayıtlar duruyor, `Plaques.Queried=false`, `History.Queried=
>    **true**` (ayrık savepoint kanıtı), **tam bir ERROR** loglanıyor; sağlıklı
>    okumada ikisi de `true` ve sıfır log.
>
> 11. **⚠️ 3. TURDA DÜZELTİLEN ÜÇ KÜÇÜK ŞEY.** (a) `signup.go`'daki *"Reproduce:"*
>    komutu (`grep -c … -A20`) **hiçbir şeyi göstermiyordu** — `-c`, `-A20`'yi yutuyor
>    ve çıktı `1`; yerine olguyu gerçekten basan **üç dosya-içi komut** kondu
>    (KF=multi/KM=single · 5 vardiyalı departman · beşinin de tenant_id'si KM).
>    (b) `panelstart_db_test.go`'daki *"all three"* sayısı **bir turda bayatladı**
>    (dört var) — türeten komut yazıldı. (c) Karttaki **ham alan sayımı atıldı**:
>    *"5/1/4"* hiçbir tek kuralla üretilemiyordu (csrf+honeypot → **5/2/5**; düz
>    `name="` → **5/2/4**; ikinci adım her iki okumada **2**).

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

**Kabul kriterleri — B FAZI (A fazının ölçümlerinden doğdu, 2026-08-14).** Bunlar
kod yorumlarında ve [ADR 0015](../adr/0015-sifirlama-tokeni-tek-gecislik-yetkidir.md)'te
de yazılı ama **denetlenecek liste burasıdır**; A fazı bunların hiçbirini
karşılamaz ve karşıladığını iddia etmez.

- **Sıfırlama isteği uç noktası ADRES BAŞINA oran sınırlı.** Tek bir sayı iki ayrı
  zararı birden kapatıyor ve ikisi de ölçüldü: (a) **kurtarma reddi** — saldırgan
  kurbanın adresini genel forma yazarak kurbanın bekleyen linkini iptal ettirir
  (`retired_count=1` → kurbanın `Consume`'u 0 satır), tekrar edilirse hesap sahibi
  kurtarmayı hiç tamamlayamaz; (b) **CPU tükenmesi** — `Consume` token'ın varlığına
  bakmadan tam bcrypt öder (**212,8 ms**, kimlik doğrulamasız sayfa). Sıra
  değiştirilerek çözülemez: önce çözümlemek bir **zamanlama kehaneti** açar.
- **Başarısız her sıfırlama denemesi bir yere düşüyor (§4.6).** Token **çözümlenen**
  denemede `audit_log` satırı (`Consume` gerekli olguları hataya ek olarak
  döndürür); **çözümlenmeyen** denemede süreç log'u — çünkü `audit_log.tenant_id`
  NOT NULL'dur (00005) ve o vakada tenant yoktur. Hiçbirinde token ya da hash
  **yazılmaz**.
- **Link yalnızca yöneticinin KENDİ satırındaki adrese gider.** Veri katmanı
  MINTING'i kapatmaz ve kapatamaz (ölçüldü: `tappa_app` kendi tenant'ında istediği
  yönetici için token basıp tüketebilir); ele geçirmeyle arasında duran tek şey ham
  token'ın çağırana **asla dönmemesi** ve tek bir alıcıya gitmesidir.
- **Bir store `*Params` değeri asla yazdırılmıyor.** Ham token redakte eder, **hash
  etmez** (`store.*Params.TokenHash`/`.PasswordHash` düz `string` — depo geneli
  desen).
- **E-postadan adaya eşleme bir numaralandırma kehaneti değil** (00011 md.1):
  kayıtlı ve kayıtsız adres için yanıt durum, gövde, ifade **ve zamanlama**
  bakımından aynı. Aday daraltma kararı buradadır ve yanlış verilirse 00017'nin
  artık-kilidini bu yüzeyde yeniden üretir.

> **Kart düzeltmesi (2026-08-14, M7-04 A fazı uygulaması sırasında).** Beş nokta
> kartla gerçek arasında ayrıştı; her biri ölçümle.
>
> 1. **"Şifre sıfırlama" YENİ TABLO GEREKTİRMİYOR — tablo 2026-07'den beri
>    duruyor.** Migration **00006** `password_resets`'i (id, tenant_id,
>    admin_user_id, token_hash, created_at, expires_at, used_at) RLS beşlisi ve
>    `REVOKE DELETE` ile birlikte yarattı. Eksik olan tablo değildi: `db/queries`
>    altında `password_resets` **hiçbir dosyada geçmiyordu** (grep), bir reset
>    LİNKİNDEN satıra ulaşmanın yolu yoktu (link yalnız token taşır → tenant
>    aramanın **sonucu**), ve yetkiler `tappa_app`'e **tablo geneli UPDATE**
>    veriyordu. **00019 bu yüzden bir sertleştirme migration'ı**, `CREATE TABLE`
>    değil — §6'nın beşli kuralının uygulanacağı yeni tablo yok.
> 2. **🔴 KARTIN GÖRMEDİĞİ ASIL AÇIK: tablo geneli UPDATE bir HESAP ELE GEÇİRME
>    ilkeliydi.** 00019 öncesi, `tappa_app` olarak kendi tenant bağlamında,
>    `BEGIN…ROLLBACK` içinde ölçüldü (yük 7.71): **A1** canlı bir sıfırlama
>    token'ının `admin_user_id`'sini **aynı tenant'ın owner'ına** çevirmek →
>    `UPDATE 1`. Yani kendisi için sıfırlama isteyen bir *manager*, elindeki linkle
>    **owner'ın parolasını** yazabiliyordu; RLS bunu göremez, çünkü aynı tenant.
>    A2 (süreyi 10 yıl uzatma), A3 (harcanmışı geri alma), A4 (bilinen hash'i
>    başkasının satırına yamama), A5 (`expires_at='infinity'`), A6 (gelecek
>    `created_at`), A7 (`token_hash='plaintext-reset-token'`) hepsi çalışıyordu.
>    **00019 sonrası aynı yedi ifade:** A1/A2/A4/A6 → `42501`, A5 → `23514`
>    (`password_resets_ttl_ceiling`), A7 → `23514`
>    (`password_resets_token_hash_shape`). **A3 KAPANMADI** — sütun grant'i hangi
>    sütunun yazılacağını söyler, hangi **değeri** değil; koruma `db/queries`
>    disiplinidir, yapı değil (00009/00012 aynı sınırı kaydediyor).
> 3. **"Magic link ve/veya şifre" bir karar boşluğuydu; A fazı kapsamı DARALTTI.**
>    Ayrık koşulun sağ kolu **M6-01'de sevk edildi** (`adminauth.Authenticate`),
>    yani kriter bugün zaten karşılanıyor; sol kol **ikinci bir kimlik doğrulama
>    yolu** = ikinci saldırı yüzeyi. Eleyen fark veri katmanında: bu tablodaki bir
>    satır **tek bir durum geçişine** izin verir (`admin_users.password_hash`'i bir
>    kez yaz) ve **oturum vermez**. Çalınan bir sıfırlama linki kurbana *gördüğü*
>    bir parola değişikliğine mal olur (kendi parolası çalışmaz, oturumları iptal
>    edilir, audit satırı vardır); çalınan bir magic link **sessiz** bir girişdir —
>    §4.6'nın reddettiği şekil. Magic link istenirse **kendi tablosu ve kendi
>    ADR'si** ile gelir; `password_resets`'e bindirmek, halihazırda dağıtılmış her
>    sıfırlama token'ını bir oturum token'ına **yükseltirdi**.
> 4. **Sıfırlama, M7-02'nin `signInBlocked` kanalının çaresi DEĞİL** — ve bunu
>    yazmak gerekiyor, çünkü öyleymiş gibi okunuyor. O kusur, müşterinin satırının
>    `created_at` sırasında `adminauth.MaxCandidates` penceresinin **dışında**
>    kalmasıdır; parolayı değiştirmek satırı pencereye **taşımaz**.
>    ⚠️ **Bu madde ikinci bir cümle daha taşıyordu ve ölçümle geri çekildi:**
>    *"sıfırlama e-postadan aday çözerse aynı kilidi miras alır."* Miras **otomatik
>    değil** — `resolve_admin_by_email`'de `LIMIT` yok (00011 açıkça yazıyor),
>    `MaxCandidates` bir Go sabiti (`manager.go:118`) ve yalnız `Authenticate`'in
>    aday döngüsü/dolgusunda kullanılıyor (442/443/579). Sıfırlama aday başına
>    bcrypt ödemez, yani o pencereyi almak zorunda değildir; **alıp almayacağı B
>    fazının kararıdır ve yanlış verilirse kilidi yeniden üretir.** Bu yüzden
>    `CreatePasswordReset` e-posta değil `admin_user_id` alır ve "hangi adaya link
>    gider" sorusu bilinçli olarak B fazındadır.
> 5. **Q02 BU FAZI BLOKLAMADI ve hiçbir parçası sağlayıcıya bağlanmadı** — kısıt
>    olarak uygulandı: veri katmanında sağlayıcı adı, API şekli ya da taşıyıcıya
>    özgü alan **yok**. *"Gönderim başarısızlığı"* için **sütun eklenmedi ve bu bir
>    karardır**: senkron SMTP hatayı hâlâ açık olan isteğe **döndürür**, kuyruklu
>    bir sağlayıcı dakikalar sonra **webhook**'la bildirir — iki farklı sütun, iki
>    farklı yaşam döngüsü, ve birini seçmek sağlayıcıyı **ima yoluyla** seçmek
>    olurdu. Repo bu soruyu zaten sütunsuz yanıtlıyor: `internal/invite.Channel`
>    hatayı çağırana döndürür ve ifşayı `audit_log`'a yazar; `employee_invites`'ta
>    da teslimat sütunu yoktur. B fazı bu emsali sürdürür.

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

---

## M7-06 — Yasal metinlerin operatör formu

- **Bağımlılık:** M7-01 (dört iskelet + `pages.LegalPages`) · M6-02 (panel kabuğu) ·
  M6-01 (`adminauth` oturumu)
- **Kırmızı çizgi:** §4.3 (append-only + `audit_log`) · §4.5 (çapraz-tenant yeteneği
  doğmamalı) · §4.7 (adres loglanmaz)
- **Commit:** `feat(legal): publish Tappa's own legal texts from the panel`

**Amaç.** M7-01'in **dört yasal metnini** (şirket künyesi · veri sorumlusu +
iletişim · saklama süreleri · sözleşme/fatura şartları) **bir formdan** girilebilir
kılmak. Bugün `web/templates/pages/legal.templ` onları iskelet olarak basıyor ve
*"Nothing on this page is in force. It is a placeholder…"* diyor; üç oturumdur
kullanıcı bekleniyor ve beklemenin sebebi unutkanlık değil **yapı**: metni girmenin
tek yolu Go kaynağını düzenlemekti.

**Kapsam dışı.** Metinlerin **içeriğini** yazmak (kullanıcının işi) · zengin editör ·
önizleme/taslak durumu · sürüm karşılaştırma ekranı · tenant düzeyi ayarlar (M7-05).

**Kabul kriterleri.**
- Panelden dört belgenin her biri ayrı ayrı **yayımlanabiliyor**; yayımlanan metin
  herkese açık `/legal/<slug>` sayfasında görünüyor ve o sayfa *"yayımlanmadı"*
  demeyi bırakıyor.
- **Kısmi durum doğru:** birini yayımlamak diğer üçünün sayfasında **tek kelimeyi**
  değiştirmiyor, ve `robots` değeri **belge başına** türetiliyor.
- Yazan kişi bir **izin listesinden** doğrulanıyor; liste **boşken hiç kimse**
  giremiyor (fail-closed), ve reddediş hem gezinmede hem **rotada** var.
- Her yayımlama `audit_log`'a düşüyor, yazma yolunda **`RecordTx` ile aynı
  transaction'da**.
- Metin **HTML olarak yorumlanmıyor**; `templ.Raw`'a gidilmiyor.
- Yeni tablo §6'nın beşlisini karşılamıyorsa **ADR** ile gerekçelendiriliyor ve
  `redline-check` muafiyeti gürültülü.

**Tuzaklar.**
- 🔴 **Bu projede süper admin YOK ve icat edilmemeli.** `admin_users.role` kapalı
  sözlüğü `('owner','manager')` (00006:66) ve ikisi de **tenant kapsamlı**; o sözlüğü
  **policy motoru** okuyor (`actor:role`). Üçüncü bir değer, motorun kuralı olmayan
  bir değerdir — üstelik her tap'in nasıl yargılandığına karar veren yolda.
- 🔴 **"Tappa'nın kendi tenant'ı" §4.5'i ihlal eder.** Operatörün hesabı bir
  **müşteri** tenant'ında; içerik başka bir tenant'ta olsaydı yazma yolu
  `WithTenant(başkasınınTenantı, …)` olmak zorundaydı. Bu yetenek doğmamalı.
- 🔴 **Herkese açık `/legal/*` bir OKUMA yolu ve `Marketing` yapısal olarak
  havuzsuz.** O tipin *"no `*db.DB` → it cannot make a database query,
  structurally"* iddiası ve **oran sınırı olmaması** aynı cümleye dayanıyor: *"it
  touches no pool"*. Oraya istek başına bir `SELECT` koymak, ürünün en çok taranan
  URL'ini check-in'in paylaştığı havuza bağlar.
- ⚠️ **`templ.Raw` bu repoda ölçülmüş bir kör nokta** — sıfır çağrı yeri, ve `.templ`
  tarayan **on iki** testin hiçbiri bir ilkini görmez.
- ⚠️ **Metin girilmemişken sayfanın söylediği şey DOĞRU.** *"Nothing on this page is
  in force"* dürüst bir cümledir; kaldırılacağı tek an, o belge için biri metin
  yayımladığı andır.

> **Sevk edildi (2026-08-14) — migration 00020, [ADR 0016](../adr/0016-tenant-kapsamsiz-operator-icerigi.md).**
> Üç ölçüm karara bağlandı; her birinde **elenen şıkkın fiyatı** yazılı.
>
> 0. **🔴 2. TURDA GERİ ÇEKİLEN CÜMLE: *"EKRAN HİÇBİR TENANT'IN VERİSİNE BAKMAZ"*.**
>    Bağımsız göz tek adımda çürüttü: `legalView → a.chrome() → a.queue.Pending(ctx,
>    id.TenantID())` **gerçek bir sorgudur** ve cevabı sayfanın Review sekmesinde basılı
>    (denetçi `100+` gördü). **§4.5 ihlali DEĞİL** — okunan tenant çağıranın **kendisi**.
>    Ama görevin bütün kırmızı-çizgi argümanı o cümleye dayandırılmıştı, yani bu tam
>    olarak bu projenin **imza kusuru**: sağlanmayan bir garantiyi beyan etmek.
>    **Sevk edilen dar cümle:** *"düzenlenen BELGELERİN `tenant_id`'si yok; ekranın
>    adlandırdığı tek tenant — audit satırında ve panel kabuğunda — çağıranın kendisidir."*
>    Üç yerde düzeltildi (`legaladmin.go` iki yer, `m9-sonrasi.md`).
>
> 1. **🔴 METİNLER TENANT KAPSAMSIZ BİR TABLODA — VE ELEYEN ÖLÇÜM §4.5.**
>    `legal_documents` (`id · slug · body · published_at · published_by`), `tenant_id`
>    **yok**. Elenen şık *"Tappa'nın kendi tenant'ı"*ydı ve fiyatı bir tercih değil bir
>    **yetenek**: operatörün admin hesabı bir müşteri tenant'ında yaşıyor, dolayısıyla
>    yazma yolu müşteri kimliğiyle ulaşılabilen bir handler'da
>    `WithTenant(başkasınınTenantı, …)` çağırmak zorunda kalırdı. Kaçınmanın tek yolu
>    **ikinci bir hesap ve ikinci bir giriş**ti. Seçilen şıkta çağıran **hiçbir zaman**
>    kendi tenant'ından başkasını adlandırmıyor. **Yapısal kanıt üretilen tiplerin
>    üstünde:** `store.PublishLegalDocumentParams`'ta `TenantID` alanı **yok**,
>    `ListPublishedLegalDocuments` yalnız `ctx` alıyor
>    (`TestLegalStore_CannotNameATenantAtAll`, **bağımsız** pozitif kontrolüyle —
>    `RecordAuditEventParams` bir tenant alanı taşımak **zorunda**, taşımıyorsa tarama
>    yanlış yere bakıyordur).
>    §6 istisnası **gürültülü**: muafiyet `redline-check.sh`'in kendi sözdizimiyle
>    (`-- redline: no-tenant-scope(legal_documents) — …`) yazıldı ve her koşuda
>    **WARN** basıyor. **Bu, o mekanizmanın repodaki İLK kullanımı** — script yazıldığından
>    beri muaf edecek bir şey yoktu. ⚠️ Muafiyet **beşli denetimin tamamını atlar**, o
>    yüzden RLS (`ENABLE`+`FORCE`+`USING (true)`) **gönüllü** yazıldı ve **canlı
>    katalogda** test ediliyor. Tablo append-only: `REVOKE UPDATE, DELETE` + 0005'in
>    trigger'ı; `published_by`'da **FK yok** (FK denetimi RLS'i görmez → çapraz-tenant
>    varlık kehaneti olurdu, `audit_log.actor_id` emsali).
>    🔴 **VE MUAFİYETİN GERÇEK FİYATI 2. TURDA ÖLÇÜLDÜ: YAZMA TARAFINDA DB DERİNLİĞİ
>    YOK.** Yabancı bir tenant bağlamında, `tappa_app` olarak, `INSERT INTO
>    legal_documents` **başarılı** (50→51→ROLLBACK); aynı bağlamda `admin_users` ve
>    `tenants` **0 satır**. Yani ürünün her yerindeki **kuşak+kemer** (RLS + açık tenant
>    filtresi) burada **yok ve olamaz** — *"kim yazabilir"* sorusunun **tek** cevabı
>    uygulama izin listesidir. Migration'ın *"yazma yetkisi GRANT'ta"* cümlesi
>    **yanıltıcıydı** (GRANT kimseyi ayırmıyor) ve düzeltildi. Ayakta kalan koruma:
>    **append-only** (yanlış yazılan satır gizlenemez) ve `slug` CHECK'i.
>
> 2. **🔴 KİM YAZABİLİR: `TAPPA_OPERATOR_ADMIN_IDS` — VE ORKESTRATÖRÜN KARARININ YARISI
>    ÖLÇÜMLE ÇÜRÜTÜLDÜ.** *"Env izin listesi"* yarısı **ayakta**; *"e-posta"* yarısı
>    **kırıldı**. Bunu bulan şey benim ölçümüm değil, `tappa-security-auditor`'un uçtan
>    uca sömürüsü oldu ve bu kayda geçiyor: **`admin_users` e-posta tekilliği TENANT
>    BAŞINADIR** (`UNIQUE (tenant_id, email)`), `/signup` **herkese açıktır**, ve repoda
>    **hiçbir e-posta doğrulaması yoktur**. Yani izin listesindeki bir adresi bilen
>    herkes o adresle **kendi işletmesini kaydedip** ekrana girebiliyordu. **Ölçüldü:**
>    yabancı tenant, rol *manager*, `GET /admin/legal` → **200**, `POST` → **303**,
>    yayımlanmış gizlilik politikası **değiştirildi**. Genel ifade: **bir izin listesi
>    ancak anahtarının tekilliği kadar değerlidir.**
>    **Sevk edilen anahtar `admin_users.id`:** PRIMARY KEY, **global tekil**, ve değeri
>    **veritabanı** atıyor (`gen_random_uuid()`; `signup` onu INSERT'ten *geri okuyor*,
>    seçmiyor) — hiçbir form/başlık/kayıt akışı **beyan edemez**. Regresyon:
>    `TestOperatorGate_IsNotJoinableByRegisteringABusiness`.
>    ✅ **Rekey bir yan fayda getirdi:** `AdminUserID` çözümlenen kimlikte **zaten
>    vardı**, dolayısıyla ilk sürümün ölçülmüş *"`TouchAdminSession`'a bir sütun ekle"*
>    takası **tamamen geri alındı** — `db/queries/admins.sql`, `internal/store/admins.sql.go`
>    ve `adminauth.Resolved` bu görevden **hiç etkilenmiyor** (`git diff` boş), ve
>    panelde kişisel veri genişlemesi olmadı.
>    ⚠️ **ERGONOMİ KAPATILDI:** reddetme sayfası **çağıranın KENDİ** admin id'sini basıyor
>    (*"TAPPA_OPERATOR_ADMIN_IDS bunu alır"*), yani kullanıcı veritabanına inmiyor. Bir
>    admin id'si sır değildir; sayfa izin listesini, boş olup olmadığını ya da başka bir
>    admin'i **asla** basmıyor (`TestOperatorRefusal_ShowsTheCallerTheirOWNIdAndNobodyElses`).
>    **Gate iki kez soruluyor ve ikisi birbirinin yerine geçmiyor** (M6-12'nin dersi):
>    sekme `PanelSection.OperatorOnly` ile gizleniyor, rota **mount edilmiş kalıyor** ve
>    handler **403** veriyor (chi'den 404 **değil**). **Fail-closed mutasyonla
>    kanıtlandı.**
>
> 2b. **🔴 DENETİMİN İKİNCİ BULGUSU: YAZMA YOLU PANELİN GÖVDE SINIRI OLMAYAN TEK POST'UYDU.**
>    Diğer sekiz panel POST'u 4–16 KiB bağlıyor; bu `r.ParseForm()`'u çıplak çağırıyordu,
>    yani Go'nun 10 MiB'ı. **Ölçüldü:** 1/4/9 MiB gövdeler **kabul edildi ve saklandı**;
>    tablo append-only olduğu için **temizlik yolu yok** (yalnız yeni migration), ve
>    `adminSessionLimit` ile oturum başına **~2,7 GB/pencere**. İkinci yarısı **herkese
>    açık sayfaydı**: 9 MB'lık gövde **anonim GET başına 253 ms** CPU (9 MB düzyazı
>    9,5 ms — fark `normalizeNewlines`'ın `for strings.Contains(…)` döngüsü).
>    **Üç düzeltme:** `http.MaxBytesReader` **256 KiB** (diğer sekizle aynı desen,
>    `MaxBytesError` dalı ve kendi cümlesiyle) · paragraf bölme **yayım anında bir kez**
>    (anlık görüntüde saklanıyor) · `normalizeNewlines` **tek geçiş**.
>
> 2c. **⚠️ VE İKİ KÜÇÜK BULGU DAHA KAPATILDI.** (a) `published_by` **her tenant'ın
>    bağlantısı tarafından okunabiliyordu** (tablo tenant kapsamsız, politika
>    `USING (true)`; denetim yabancı bağlamda 37 satır okudu, aynı bağlamda `admin_users`
>    0) — ADR 0016 §4'ün FK'yi reddettiği kehanetin zayıf bir sürümü. 00020 artık **sütun
>    düzeyinde** yetki veriyor: `INSERT (slug, body, published_by)` ama
>    `SELECT (id, slug, body, published_at)` — uygulama yazar, **hiç okuyamaz**.
>    (b) `Publish` anlık görüntüyü commit'ten sonra kuruyordu, yani eşzamanlı iki
>    yayımlama **geriye** gidebiliyordu; yayımlama artık bir mutex altında seri
>    (yol ömürde birkaç kez koşuyor, `Published()` kilitsiz atomic kalıyor).

> 3. **🔴 RENDER: `[]string` + `for` + `{ }`. `templ.Raw`'a GİDİLMEDİ.** Ölçüldü:
>    `{ expr }` → `templ.EscapeString` → `html.EscapeString` (beş karakter), ve satır
>    sonlarına **hiçbir şey yapmıyor** — yani metin escape'li ama tek paragraf olurdu.
>    `internal/domain/legal.Paragraphs` boş satırlarda bölüp **string** döndürüyor;
>    şablon üstünde dönüyor. `templ.Raw` bu repoda **sıfır** çağrı yerine sahip ve
>    `.templ` tarayan **on iki** testin **hiçbiri** bir ilkini görmez
>    (`policies_test.go` kendi kör noktasını yazıyor). ⚠️ **CRLF tuzağı gerçek:**
>    tarayıcı `<textarea>`'yı CRLF ile gönderir, yani yalnız `\n\n` bilen bir bölücü
>    tam da hizmet ettiği tek girdide **hiç paragraf bulamaz**. Mutasyonla doğrulandı.
>    Markdown **reddedildi**: bağımlılık ister (go.mod'da beş var, sanitizer yok) ve her
>    ciddi renderer HTML üretir — yani fazladan adımlı bir `templ.Raw`.
>
> 4. **⚠️ HERKESE AÇIK YOL HAVUZA DOKUNMUYOR — VE BU BİR KARARDI.** `handler.Marketing`
>    üçüncü bir alan aldı (`legalReader`) ve `TestMarketing_HandlerHoldsNoStatefulDependency`'nin
>    izin listesine **gerekçesiyle** eklendi. Alan bir **anlık görüntü okuyucusu**:
>    tek metodu **`context.Context` almıyor ve `error` döndürmüyor** — iptal edilemeyen
>    ve başarısız olamayan bir metot I/O yapmıyordur. `TestLegalReader_CannotReachTheDatabase`
>    imzayı reflection'la okuyor, **bağımsız pozitif kontrolüyle** (`panelTexts.Publish`
>    hem argüman hem `error` taşımak zorunda). **Elenen şık:** `/legal/*`'a istek başına
>    `SELECT`. Fiyatı sayıldı: o sayfaların **oran sınırı yok** ve bu, `marketing.go`'nun
>    *"it touches no pool"* cümlesine dayanıyor; oraya bir sorgu koymak ürünün en çok
>    taranan URL'ini **check-in'in paylaştığı havuza** bağlardı.
>    ⚠️ **ANLIK GÖRÜNTÜNÜN FİYATI, kapatılmadan sayıldı:** yayımlayan **süreç**
>    tazeliyor. **İkinci bir süreç** yeniden başlayana kadar eski metni sunar. Bugün tek
>    VPS (§1) → tek süreç; ikinci süreç bunu **yanlış** yapar ve çaresi `Store.Refresh`'i
>    bir zamanlayıcıya bağlamaktır. Boot okuması **ölümcül değil**: başarısızsa dört sayfa
>    M7-06 öncesi hâllerine düşer (*"yayımlanmadı"*) — **eksik iddia** eder, ki bu ürünün
>    yanılması gereken yön.
>
> 5. **⚠️ BOOT OKUMASI HİÇBİR TENANT ADLANDIRMIYOR.** `WithTenant` nil uuid'i reddediyor
>    ve açılışta doğal bir tenant yok. İki aday vardı: **gerçek bir tenant** (callback
>    canlı bir müşterinin satırlarını görürdü) ve **hiçbir tenant'la eşleşmeyen bir
>    uuid**. İkincisi seçildi: o bağlamda RLS kapsamlı **her** tablo sıfır satır döner,
>    yani ulaşılabilen tek şey politikası `USING (true)` olan tablodur.
>    `TestLegalDB_RefreshNeedsNoTenantAndSeesNothingElse` iki yönü de ölçüyor (metin
>    okunuyor; `admin_users` sayımı **0**).
>
> 6. **⚠️ SAYFANIN SÖYLEDİĞİ HER CÜMLE, VE KISMİ DURUM.** *"Nothing on this page is in
>    force"* **belge başına** kalkıyor; `Published()` tanımı `len(Needs)==0`'dan
>    **`len(Body)>0`**'a taşındı (eski tanım, metinler Go kaynağındayken doğruydu).
>    `robots` aynı olgudan türüyor, yani biri yayımlanınca **o sayfa** indekslenebilir
>    olur, diğer üçü **noindex** kalır.
>    `TestLegalPages_PublishingOneDocumentChangesThatDocumentAndNoOther` diğer üç sayfayı
>    **bayt bayt** karşılaştırıyor.
>    ⚠️ **İDDİA EDİLMEYEN ŞEY:** metnin **yeterli** olduğu. Ürün bir gizlilik politikasını
>    yargılayamaz; garanti dar — sayfa *"yayımlanmadı"* demeyi **ancak biri kasten
>    yayımladığı için** bırakır. Operatör *"TODO"* yapıştırırsa sayfa onu basar.
>
> 7. **⬜ KAPATILMAYAN, FİYATIYLA.** (a) **Sürüm geçmişi ekranı yok** — tablo append-only,
>    yani her sürüm duruyor, ama onu **gösteren** hiçbir sorgu/ekran yok; ayrıca
>    `published_by` **uygulama rolü tarafından okunamıyor** (00020 sütun yetkisi), yani
>    *"bu metni kim yayımladı"* sorusu ürün içinde cevaplanamaz — cevabı `tappa_owner`
>    ile bir adli okuma ya da tenant başına `audit_log`. Fiyatı: bir sorgu + bir ekran
>    (+ okumanın istenip istenmediği kararı). (b) **Yayımlamanın geri alması yok** —
>    düzeltme yeni satırdır ve doğrusu budur, ama *"yanlışlıkla yayımladım, iskelete
>    dönsün"* yolu **yok**; fiyatı ya bir `withdrawn_at` sütunu (append-only'yi bozar)
>    ya da *"boş metin"* kavramı (00020'nin CHECK'i bilerek reddediyor).
>    (c) **Yayımlama oran sınırlı değil** — panelin `sessionGate`'i (10 dk'da 300) ve
>    artık **256 KiB gövde tavanı** iki fren; en kötü hâl oturum başına ~75 MB/pencere
>    (öncesi ~2,7 GB) ve tablo hâlâ temizlenemiyor. (d) `TestLegalScreen_*` **ekran
>    metnini** denetler, **doğruluğunu** değil. (e) **Anlık görüntü tek süreç
>    varsayar** — 4. maddedeki sınır.
>
> 7b. **🔴 MUTASYON SAYISI 2. TURDA DÜZELTİLDİ: "10/10" YANLIŞTI, GERÇEĞİ 9/10 İDİ.**
>    Bağımsız göz sağ kalan mutasyonu buldu: `marketing.go`'da `v.Body =
>    legal.Paragraphs(d.Body)` (render'da **yeniden bölme**) — çıktı **birebir aynı**
>    olduğu için hiçbir davranış testi göremez, ve iki paket de yeşil kalıyordu. Yani
>    ADR 0016 §6'nın üç düzeltmesinden biri (**precompute**) render ağında tutulmuyordu.
>    **Kapatıldı, sınır olarak yazılmadı:** iddia davranışsal değil **yapısal** olduğu
>    için kanıtı da yapısal — `TestLegalPublicPath_WritesNothing`'in zaten `marketing.go`'yu
>    gezen go/ast taraması artık `.Publish` yanında **`.Paragraphs`** çağrısını da
>    reddediyor (aynı anti-vacuity kancası: tarama `.Published`'ı görmezse zaten
>    `t.Fatal`). **Mutasyonla doğrulandı:** eski satır geri konunca test
>    `marketing.go:237` diyerek kırmızıya dönüyor. **Sayı artık 11 mutasyon / 11
>    yakalandı** — ve bu satır, elle tutulan bir sayının bu repoda kaçıncı kez
>    bayatladığının kaydıdır.
>
> 7c. **⬜ ÖLÇÜLDÜ, KARAR KOORDİNATÖRÜN: `REVOKE INSERT (id) ON admin_users`.**
>    İzin listesinin anahtarı artık `admin_users.id`, ve `tappa_app` **ayrıcalık
>    düzeyinde** hâlâ seçtiği id ile satır ekleyebiliyor (denetçi ölçtü). **Bugün
>    kapalı ama sorgu metniyle kapalı:** üretimde `admin_users`'a **tek** INSERT var
>    (`CreateAdminWithTenant`, `db/queries/admins.sql:306`) ve `id` sütununu
>    **adlandırmıyor** — başka üretim yolu yok.
>    **Kapatmanın ölçülen fiyatı:** `id`'yi açıkça yazan **~20 test dosyası** var
>    (`internal/db`, `internal/adminauth`, `internal/handler`, `internal/domain/`
>    signup/billing/tenant/legal) ve hepsi `tappa_app` olarak koşuyor → bir REVOKE
>    hepsini kırar, yani M7-06'nın kapsamından çok daha geniş bir fixture yeniden
>    yazımı.
>    **Ve kazancı ölçülünce küçülüyor:** `INSERT (id)`'yi sömürebilen saldırgan zaten
>    `tappa_app` olarak keyfî SQL koşturabiliyordur, ve o saldırgan `legal_documents`'a
>    **doğrudan** yazabilir (1. maddedeki *"yazma tarafında DB derinliği yok"* ölçümü) —
>    yani REVOKE o tehdit modeline karşı **artımlı hiçbir şey** kazandırmıyor.
>    **Önerim: migration DEĞİL, M9-08'e limit.** Kapsam kararı koordinatörün; migration
>    00021 istenirse yazılır.
>
> 8. **🔴 GÜVENLİK DENETİMİ İKİ BLOKLAYAN BULGU ÇIKARDI VE İKİSİ DE BU GÖREVDE
>    KAPANDI** — ayrıntı 2. ve 2b/2c maddelerinde. Ders olarak yazılıyor çünkü ikisi de
>    **benim ölçtüğüm şeyin yanındaki** şeydi: (i) anahtarın **maliyetini** ölçtüm
>    (e-posta çözümlenen kimlikte yok → bir sütun), **tekilliğini** ölçmedim; (ii) yazma
>    yolunun **yetkisini** ve **transaction'ını** ölçtüm, **gövde boyutunu** ölçmedim —
>    oysa diğer sekiz panel POST'unun hepsinde o sınır var, yani bu *"kalıbın yarısını
>    kopyalama"* kusurunun bir başka örneğiydi. **Genel kural:** bir izin listesi
>    eklerken *"anahtarı kim atıyor"* sorusu *"anahtarı nereden okuyorum"* sorusundan
>    önce gelir.
