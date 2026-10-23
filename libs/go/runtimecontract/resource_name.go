package runtimecontract

// ValidDNSLabel checks a single resource-name label of one to sixty-three bytes using only
// lowercase ASCII letters, digits and internal hyphens, preserving the runtime naming grammar.
// It neither normalizes the supplied name nor resolves DNS or authorizes a Kubernetes resource.
// ValidDNSLabel проверяет отдельную метку имени ресурса длиной от одного до шестидесяти трёх байтов,
// разрешая только строчные ASCII-буквы, цифры и внутренние дефисы согласно грамматике runtime.
// Имя не нормализуется, DNS не разрешается, доступ к ресурсу Kubernetes не выдаётся.
func ValidDNSLabel(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-'
		if !valid || character == '-' && (index == 0 || index == len(value)-1) {
			return false
		}
	}
	return true
}
