package platform

import _ "embed"

var (
	//go:embed sql/permissions_authorizecommand_1.sql
	queryPermissionsAuthorizecommand1 string
	//go:embed sql/permissions_requireprojectpermission_1.sql
	queryPermissionsRequireprojectpermission1 string
	//go:embed sql/permissions_projectidbyresource_1.sql
	queryPermissionsProjectidbyresource1 string
	//go:embed sql/permissions_projectidbyresource_2.sql
	queryPermissionsProjectidbyresource2 string
	//go:embed sql/permissions_projectidbyresource_3.sql
	queryPermissionsProjectidbyresource3 string
	//go:embed sql/permissions_projectidbyresource_4.sql
	queryPermissionsProjectidbyresource4 string
	//go:embed sql/permissions_projectidbyresource_5.sql
	queryPermissionsProjectidbyresource5 string
	//go:embed sql/permissions_projectidbyresource_6.sql
	queryPermissionsProjectidbyresource6 string
	//go:embed sql/permissions_projectidbyresource_7.sql
	queryPermissionsProjectidbyresource7 string
	//go:embed sql/permissions_projectidbyresource_8.sql
	queryPermissionsProjectidbyresource8 string
	//go:embed sql/permissions_projectidbyresource_9.sql
	queryPermissionsProjectidbyresource9 string
)
