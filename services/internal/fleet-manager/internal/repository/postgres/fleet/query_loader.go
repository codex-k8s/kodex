package fleet

import "fmt"

func mustLoadQuery(name string) string {
	switch query, err := loadQuery(name); {
	case err != nil:
		panic(err)
	default:
		return query
	}
}

func loadQuery(name string) (string, error) {
	fileName := fmt.Sprintf("sql/%s.sql", name)
	data, err := SQLFiles.ReadFile(fileName)
	if err != nil {
		return "", fmt.Errorf("read fleet sql file %s: %w", fileName, err)
	}
	return string(data), nil
}
