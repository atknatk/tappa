# ADR 0033 — Yayımlanan yasal metinde bağlantı: kapalı dilbilgisi, ham HTML yok

- **Durum:** Kabul edildi (2026-10-23)
- **Karar veren:** kullanıcı isteği (2026-10-23: imprint'teki e-posta, telefon ve web adresi
  tıklanabilir olsun — "HTML enjekte edeyim"); orkestratör kararı: bağlantı evet, ham HTML hayır.
- **İlgili:** ADR 0020 §7 (yasal metni platform operatörü yayımlar), ADR 0030 (rehberde bağlantı
  süzgeci — aynı ilke), CLAUDE.md §1 (yeni bağımlılık sorulur), §9;
  `internal/domain/legal` (`links.go`), `web/templates/pages/legal.templ` (`legalBody`)

## Bağlam

`/operator/legal`'da yayımlanan metin (gizlilik, şartlar, imprint, çerez) `/legal/*`'da herkese
açık, ölçümsüz bir sayfada çizilir. Bugüne kadar metin yalnız paragraflara bölünüyor ve her
karakter templ'in `{ }` kaçışından geçiyordu; `templ.Raw`'ın depoda **sıfır** çağrısı var ve
`.templ` tarayan testlerin hiçbiri ilkini fark etmez. Sonuç: bir e-posta, telefon ya da web adresi
düz metin kalıyordu.

## Karar

1. **Kapalı bir bağlantı dilbilgisi**, Markdown değil. Paragraf metninde yalnız dört biçim tanınır:
   `[etiket](hedef)`; çıplak `http://`/`https://` adresi (sondaki `.,;:!?'` ve eşi olmayan `)`
   hariç); çıplak e-posta (`mailto:`); `+` ile başlayan, rakamları arasında tek boşluk/tire olan,
   8–15 rakamlı telefon (`tel:+<rakamlar>`). `+`'sız rakam dizisi (tarih, tutar, sicil, KDV no)
   bağlantı **olmaz**. İç içe yapı, vurgu, başlık, liste yok.
2. **İzin listesi** (`legal.SafeHref`): yalnız `http`/`https` (host'lu), `mailto`, `tel`; şema
   büyük-küçük harf duyarsız, `net/url` ile ayrıştırılır; kontrol karakteri, boşluk veya ters bölü
   taşıyan hedef, `javascript:`, `data:`, `vbscript:`, göreli ve `//host` hedef reddedilir.
   Reddedilen `[etiket](hedef)` **bütünüyle** düz metin kalır. templ.URL de href'i süzer, ama o
   bizim değil; sözleşme ve testler bu listededir.
3. **Veri, HTML değil:** `legal.Paragraphs` artık `[]Paragraph` (`[]Span{Text, Href}`) döndürür;
   şablon span'ları döner, metni `{ }` ile, href'i `templ.URL` ile yazar. `templ.Raw` yine sıfır,
   yeni bağımlılık yok. Hesap yine **yayında bir kez** (snapshot kurulurken), istek başına değil.
4. Dış (`http/https`) bağlantı aynı sekmede açılır, `rel="noopener noreferrer"` taşır.

## Neden ham HTML değil

go.mod'da HTML temizleyici yok; ham HTML'i güvenle almak bir bağımlılık (ve onun bakımı) demek.
Temizleyicisiz ham HTML, operatör hesabının ele geçirilmesini **herkese açık sitede kalıcı XSS**'e
çevirir — en çok taranan, kimliksiz, oran sınırsız yüzeyde. Kullanıcının istediği (tıklanabilir
adresler) dört biçimle karşılanıyor; geri kalan HTML gücüne ihtiyaç yok.

## Sonuçlar

- Yayımlanan bir metin artık `<a href>` üretebilir, ama yalnız dört şemaya; operatör ekranının
  ipucu dilbilgisini söyler.
- `/legal/*` dışarı **bağlantı** verebilir; bu bir istek değildir (sayfa dışarıdan bir şey
  yüklemez), çerez metnindeki "üçüncü tarafa istek yok" cümlesi doğru kalır.
- Dilbilgisini büyütmek (vurgu, liste, başka şema) bu ADR'nin değişikliğidir.
- Testler: `TestParagraphs_LinksTheClosedGrammarAndNothingElse`, `TestSafeHref_AllowsOnlyTheFourSchemes`,
  `TestParagraphs_EveryHrefPassesTheAllowList`, `TestParagraphs_LinkingNeverLosesACharacter`
  (`internal/domain/legal`), `TestLegalPublicPage_LinksAreEscapedAndAllowListed` (`internal/handler`).
