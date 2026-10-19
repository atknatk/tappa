# ADR 0030 — Entegrasyon rehberi sitede HTML: goldmark, tek kaynak

- **Durum:** Kabul edildi (2026-10-19)
- **Karar veren:** kullanıcı (2026-10-19: md → HTML için `github.com/yuin/goldmark`, yeni
  bağımlılık onaylandı; OpenAPI için sayfada indirme linki, etkileşimli referans/JS yok)
- **İlgili:** ADR 0028 (§13 API host'u; "API-12 uygulama notu" — rehber), CLAUDE.md §1
  (Node yok; yeni bağımlılık sorulur), §9 (marka), M11 DOCS-1

## Bağlam

Entegrasyon rehberi (`docs/api/integration-guide.md` ve `.tr.md`) API-12'den beri API
host'unda Markdown olarak sunuluyor (`GET /v1/guide.md`, `/v1/guide.tr.md`). Bir
entegratörün ve bir müşterinin onu tarayıcıda okuyabilmesi, panelden (API keys, webhook)
oraya bağlanabilmesi için ürün sitesinde HTML bir sayfa gerekiyor. Kaynak **tek** kalmalı:
iki ayrı metin (md + elle yazılmış HTML) birbirinden kayar, ve rehberin kendi testleri
(`TestGuide_*`) md'yi sözleşme olarak okur.

Seçenekler: (a) md'yi elle HTML'e çevirmek — ikinci kaynak; (b) derlemede bir Node aracı —
§1 yasak; (c) Go'da bir Markdown kütüphanesi, süreç başına bir kez.

## Karar

1. **Bağımlılık:** `github.com/yuin/goldmark` **v1.8.6** (2026-09-03 etiketi; kendi
   bağımlılığı yok; `go 1.22`). Yalnız çekirdek (CommonMark) + goldmark'ın **kendi**
   `extension.Table`'ı (GFM tablo). Başka goldmark modülü (highlight, frontmatter, emoji,
   linkify, attribute sözdizimi…) **eklenmez**; eklemek bu ADR'nin değişikliğidir.
2. **Tek kaynak:** `docs/api` gömülü baytları hem `/v1/guide.md`'yi (değişmeden) hem
   `/docs/api` ve `/docs/api/tr` HTML sayfalarını besler. Çeviri `internal/apiguide`'da,
   `handler.NewMarketing` içinde **süreç başına bir kez**; istek başına çeviri yok.
3. **Sınırlar (güvenlik):**
   - **Ham HTML kapalı:** renderer `html.WithUnsafe` olmadan kurulur; md'deki ham HTML
     blok/etiketi goldmark'ın "raw HTML omitted" yorumuna döner. Rehberin bilerek kullandığı
     tek HTML biçimi — başlıktan önceki `<a id="x"></a>` satırı — geçirilmez: AST'de okunur
     ve o başlığın `id`'si olur. HTML yorum blokları (rehberin test işaretleri) düşer.
   - **Bağlantı:** yalnız `http(s)` (host'lu), site kökü `/…` ve `#…` çizilir; başka şema
     (`javascript:`, `data:`, `mailto:`, `ftp:`, `tel:`…), `//host` ve eşlenmemiş göreli
     dosya yolu **metin** olarak çizilir. goldmark'ın kendi tehlikeli-URL süzgeci
     `mailto:`/`ftp:`/`tel:`'i geçirdiği için kural bizimdir. Görseller metne döner (sayfanın
     CSP'sinde `img-src` yok). Tablo hizası hiç yazılmaz (`style=` yok).
   - Çevrilmiş gövde sayfada kaçışsız yazılan **tek** bloktur; onu yalnız
     `apiguide.Render` üretebilir (`Guide.body` dışa kapalı).
4. **Yer:** ürün host'unda, kimliksiz, `handler.Marketing` yüzeyinde (yasal sayfaların
   çerçevesi, CSP'si, `Cache-Control: public, max-age=300`, `index, follow`). Operatör
   host'unda ve API host'unda 404 (iki host kapısı). API host'u HTML sunmaz.
5. **OpenAPI:** `TAPPA_API_HOST` varsa sayfa `https://<api host>/v1/openapi.yaml`'a
   bağlanır (bir gezinme bağlantısı, sayfa onu yüklemez); yoksa link çizilmez — API host'u
   olmayan dağıtımda `/v1` hiç kurulmaz (ADR 0028 §13), ürün host'unda ikinci bir kopya da
   açılmaz.

## Sonuçlar

- `handler.Marketing` bir değer tipi taşır (`apiguide.Guide`); yansıma testi
  (`TestMarketing_HandlerHoldsNoStatefulDependency`) bunu adıyla değil, içinde işaretçi /
  arayüz / fonksiyon / map / kanal olmadığını yürüyerek kabul eder.
- Testler: `internal/apiguide` (ham HTML, şema, anchor, eşleme, iki dil eşliği, kaynağa
  dokunmama) ve `internal/handler/apiguidepage_test.go` (sayfalar, dil geçişi, OpenAPI
  linki, alt bilgi ve panel linkleri, host kapıları). Yapısal pazarlama testleri
  (`publicSurfaceURLs`) iki sayfayı da ölçer.
- **Bulunan kusur (kayıt):** İngilizce md'nin §9.9'unda bir satır `+ \`version\`` ile
  başlıyor; CommonMark (GitHub dahil) bunu madde imi okur. Düzeltme md'de ve
  `/v1/guide.md` baytlarını değiştirir; DOCS-1 o baytlara dokunmadığı için ayrı iş.
  `TestRender_TheTwoLanguagesAreOneDocument` bu tek farkı pinler ve md düzeltilince
  istisnanın silinmesini ister.
