package taxrule

// labels renders a rule's label key in an Invoice's locale. A key an operator
// adds without a translation prints as itself rather than blocking the issue.
var labels = map[string]map[string]string{
	"tax.kdv_20":                   {"en": "KDV (VAT) 20%", "tr": "KDV %20"},
	"tax.kdv_10":                   {"en": "KDV (VAT) 10%", "tr": "KDV %10"},
	"tax.export_service_exemption": {"en": "Export-service exemption", "tr": "Hizmet ihracatı istisnası"},
}

const defaultLocale = "en"

// Label answers the key in the locale, English when the locale has no
// translation, the key itself when nothing does.
func Label(key, locale string) string {
	byLocale, ok := labels[key]
	if !ok {
		return key
	}
	if label, ok := byLocale[locale]; ok {
		return label
	}

	return byLocale[defaultLocale]
}
