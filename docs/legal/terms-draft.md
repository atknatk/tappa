# Kullanım şartları — TASLAK İSKELET + kurucu fiyat maddesi (backlog T147)

> 🔴 **TASLAKTIR. HUKUKİ GÖRÜŞ DEĞİLDİR. YAYINDAN ÖNCE BİR AVUKAT OKUMALIDIR.**
> Depoda daha önce bir kullanım şartları metni yoktu (aranan: `docs/`, `legal_documents` canlıda
> yalnız `imprint` taşıyor — T117 ölçümü 2026-10-21). Bu dosya bir **iskelet** ve tek bir **dolu madde**
> taşır: §5, kurucu müşterilerin fiyat taahhüdü (kullanıcı kararı K5, 2026-10-22: taahhüt kalır,
> **kurucuya sınırlı**, Terms'e yazılır). Öteki maddeler yalnız ürünün bugün gerçekten yaptığını söyler;
> ticari ve hukuki her boşluk `[DOĞRULA: …]` ile işaretli. **İçinde tek bir `[DOĞRULA` kalmış metin
> yayımlanmaz.**

**Ne için:** `legal_documents`'in `terms` belgesi. Yayın kullanıcının işidir: `/operator/legal` →
*Terms* → metni yapıştır → yayımla. Biçim kuralları gizlilik taslağındakiyle aynı
(`docs/legal/privacy-draft.md` → *Biçim*): düz metin, boş satır = paragraf, Markdown yok, üst sınır
256 KiB.

## §5'in dayandığı olgular — ürün neyi garanti ediyor, neyi etmiyor (okundu)

| Olgu | Kaynak |
|---|---|
| Fiyat **işletme satırında** durur (`tenants.price_per_employee_month`, `numeric(10,2)`, varsayılan 1.50); sabit bir Go sabiti değildir — liste fiyatı değişse kurucunun fiyatı değişmesin diye | `db/migrations/00016` §1 |
| Uygulama rolü (`tappa_app`) fiyatı ve planı **ne yazabilir ne ekleyebilir** (REVOKE UPDATE/INSERT, sütun listesi dışı) | `00016` §1; `00024` |
| Fiyatı yazan **hiçbir ürün yolu yok** — operatör yüzeyi dahil; yalnız veritabanı sahibi elle yazabilir | T268 satırı; ADR 0043 §1 (plan için ölçüldü, fiyat için okundu) |
| Yani "kilit" bir **yazıcı yokluğudur**, saklanan bir söz değil: ürün geleceğe dair bir fiyat sözü tutmaz | `web/templates/pages/landingview.go` `FactPriceLockedForLife` yorumu |
| Bugün **her yeni işletme** `founding` planıyla doğar (`tenants.plan` DEFAULT `'founding'`) | `00001`; `landingview.go` `FactSignupMonthAndTheNextAreFree` |
| Ücretsiz aylar: kayıt ayı + sonraki iki ay; ay işletmenin kendi saat diliminde | `00016` `tappa_first_chargeable_month` (`+ interval '3 months'`) |
| Kişi sayımı: ay içinde herhangi bir anda aktif olan herkes; daveti tamamlamamış olan sayılmaz | `00016` `tappa_employee_is_billable`; `pricingview.go` `PricingFAQ` |
| Plan değişikliği bir sonraki faturalama ayından geçerli; başlamış ay değişmez | ADR 0043 §2 (seçenek A, kullanıcı onayı 2026-11-09) |
| Fiyat değişikliği bugün **kapatılmamış** bir ayı yeniden fiyatlar (plan için kapandı, fiyat için açık) | T268 |
| Handoff'un teklifi: *"ilk 3 ay ücretsiz + ömür boyu fiyat sabit; karşılığında referans + vaka çalışması"* | `docs/handoff.md` §11 |
| Sitedeki cümle: *"Founding customers: price locked for life."* (`/` ve `/pricing` fiyat kartı) | `landingview.go` `LandingPriceLockedForLife` |

🔴 **§5'in en büyük açığı (T286):** teklifin **kapanışı yok**. Plan varsayılanı `founding` olduğu sürece
bugünden sonra kaydolan her işletme kurucu sayılır ve fiyatı ömür boyu kilitli olur. K5'in "kurucuya
sınırlı" şartı ancak teklifin bir bitişi (tarih, işletme sayısı ya da operatör kararı) ve o andan sonra
yeni işletmenin `founding` olmayan bir planla doğması ile anlam kazanır — bu bir ürün kararıdır ve
`founding`→başka plan geçişini yazan bir yol (OP-17) bugün yok.

## Yapıştırılacak metin

```text
[TASLAK — YAYINDAN ÖNCE BU PARAGRAFI VE BÜTÜN DOĞRULA İŞARETLERİNİ KALDIR. Bu metin hukuki görüş değildir; bir avukat onaylamadan yayımlanmaz.]

These terms are the agreement between Taptime and the business that subscribes to it (the "Customer").

1. The parties

Taptime is provided by [DOĞRULA: sözleşme tarafı tüzel kişi — unvan, hukuki biçim, sicil numarası, kayıtlı adres, VAT numarası; yayımlanmış "Company details" metniyle aynı]. The Customer is the business named on the account, identified by the VAT number given at sign-up.

2. The service

Taptime records when the Customer's staff start and finish work. The Customer mounts Taptime plaques at its venues; staff activate Taptime on their own phones and clock in and out by touching a plaque. No app installation is needed. The Customer's managers see, review and export the records in the Taptime dashboard.

Every clock-in is checked before it is accepted. A record that cannot be confirmed is still kept and is placed in a queue for the Customer's managers to decide; it is never silently approved and never silently lost.

Records cannot be edited or deleted once written. A correction is a new record next to the original, and the change is logged.

The integration interface (API and webhooks) is switched on for a Customer by Taptime on request. [DOĞRULA: ücretli mi, ücretsiz mi; ayrı şartları var mı.]

3. Accounts and users

The Customer's owner creates the account and can invite further dashboard users. The Customer is responsible for who it invites, for keeping its staff list up to date, and for switching off people who no longer work for it. [DOĞRULA: müşterinin yükümlülükleri, kabul edilebilir kullanım.]

Each business's records are kept apart from every other business's, and only the Customer's own dashboard users see them, except where the Customer belongs to a group of businesses in Taptime, as the privacy notice describes. Through the product, Taptime's staff reach them only through a separate operator console, and every such access is logged.

People who administer the server and the database have technical access to all data stored there, including the database and the backups. [DOĞRULA: kim (kişi/şirket), hangi durumda ve nasıl kayıt altına alınıyor — deploy kimliğinin tappa-postgres-0 üzerinde pods/exec yetkisi var (deploy/k8s/01-rbac.yaml) ve runbook kubectl … psql kullanıyor (deploy/README.md); bu erişim operator_audit_log'a düşmez.]

The Customer is the data controller for its staff's records and Taptime processes them on its behalf. [DOĞRULA: veri işleme sözleşmesi (DPA) — Q23; bu şartlara ek mi, ayrı mı.]

4. Price and billing

The price is per person per month. The list price is EUR 1.50 per person per month. [DOĞRULA: KDV hariç mi dahil mi; para birimi ve fatura dili.] Plaques are included in the price and replaced free of charge. [DOĞRULA: kayıp/kırık plaket, kargo, plaketin mülkiyeti, adet sınırı.]

The price that applies to the Customer is the one recorded for its account. The Customer's Billing screen shows it.

A person is counted in a month if they were active at any point in that month: from the moment they finished activation until they were switched off. A person who was invited but never finished activation is not counted. Months are calendar months in the Customer's own time zone, as set on the account.

A month's figures can be frozen by an owner once the month is over. A frozen month never changes afterwards. Every month, frozen or not, can be downloaded as CSV from the Billing screen.

A change of plan takes effect from the first billing month that starts after the change. A month that has already started is never re-priced by a change of plan.

[DOĞRULA: fatura ne zaman kesilir, ödeme süresi, ödeme yöntemi, gecikme ve ödememe sonuçları. Bugün üründe fatura kesme veya ödeme alma yolu yok; Billing ekranı yalnız sayımı ve tutarı gösterir.]

5. Founding customers

5.1 Who is a Founding Customer. A Founding Customer is a Customer whose account was created while the founding offer was open and is recorded on Taptime's founding plan. The founding offer is open until [DOĞRULA: teklifin kapanışı — tarih, işletme sayısı ya da Taptime'ın duyurusu. Bugün her yeni hesap founding planıyla açılıyor (migration 00001); kapanış olmadan "yalnız kuruculara" sınırı hiçbir şeyi sınırlamaz — backlog T286].

5.2 Free months. A Founding Customer pays nothing for the calendar month in which its account was created and for the two calendar months after it, counted in the Customer's time zone. The Billing screen names the first month that is charged for.

5.3 Price locked for life. The price per person per month recorded for a Founding Customer's account when the account was created will not be increased for as long as that account exists. [DOĞRULA: "for life" neyin ömrü — hesabın mı, sözleşmenin mi; kilidi ne bitirir: fesih, ödememe, işletmenin devri/satışı, hesapların birleşmesi; aynı işletmenin yeni bir hesabı kurucu sayılır mı.]

5.4 What the lock covers. The lock covers the price per person per month. It does not fix the number of people counted, which follows section 4. It does not cover taxes such as VAT [DOĞRULA], or any separately priced service Taptime may offer in future [DOĞRULA: bugün ayrıca fiyatlanan bir hizmet yok]. It does not prevent Taptime from lowering the price.

5.5 Changes of plan. If a Founding Customer's account moves to another plan, [DOĞRULA: kilit sürer mi biter mi. Üründe fiyat plandan ayrı saklanır; plan değişikliği fiyatı kendiliğinden değiştirmez. Plan değiştiren bir ürün yolu bugün yok (OP-17).]

5.6 Conditions. [DOĞRULA: docs/handoff.md §11 teklifi "karşılığında referans + vaka çalışması" diye veriyor. Bu bir koşul mu (o zaman yerine getirilmezse ne olur), ayrı bir anlaşma mı, yoksa düşüyor mu? 5.3 bu haliyle koşulsuzdur.]

6. Suspension

Taptime may suspend an account [DOĞRULA: hangi gerekçelerle — ödememe, kötüye kullanım, güvenlik]. While an account is suspended, its dashboard cannot make changes, but staff can still clock in and out and their records are still kept. [DOĞRULA: ADR 0025'in istisna listesi — askıda da açık kalan işlemler.]

7. Ending the agreement

[DOĞRULA: fesih bildirimi süresi, her iki tarafın fesih hakları, hesap kapandıktan sonra kayıtlara ne olacağı (dışa aktarma süresi, silme/anonimleştirme). Bugün üründe hesap kapatma ve veri silme akışı yok; saklama süresi Q13'te karara bağlı değil.]

8. Service commitment

[DOĞRULA: verilen bir hizmet seviyesi taahhüdü (erişilebilirlik, destek süresi) var mı; yoksa açıkça "taahhüt yoktur" denmeli. Ürün tek sunucuda çalışır ve günlük şifreli yedek alır.]

9. Liability

[DOĞRULA: sorumluluk sınırı ve istisnaları — avukat yazmalı.]

10. Changes to these terms

When these terms change, the new version is published here with its date, and earlier versions are kept. [DOĞRULA: değişikliklerin müşteriye nasıl ve ne kadar önceden bildirileceği; 5. maddenin değişikliklere karşı korunması.]

11. Governing law

[DOĞRULA: uygulanacak hukuk ve yetkili mahkeme.]
```

## Sitedeki cümleyle uyum

Sitedeki cümle (*"Founding customers: price locked for life."*) §5.1 + §5.3 ile birebir örtüşür:
kilit yalnız kurucuya söylenir ve kayıtta yazılan fiyatı kapsar. §5.2'nin ücretsiz ay cümlesi sitedeki
*"Your signup month and the two after it are free."* (`LandingFreeMonthsLine`) ile aynı aritmetiği söyler.
Yayımlanan metin bu iki cümleden farklı bir şey söylerse site ile şartlar çelişir — biri değişirse
öteki de gözden geçirilir.
