
package generated

type RoleImagePlatformOs uint

const (
  RoleImagePlatformOsLinux RoleImagePlatformOs = iota
)

// Value returns the value of the enum.
func (op RoleImagePlatformOs) Value() any {
	if op >= RoleImagePlatformOs(len(RoleImagePlatformOsValues)) {
		return nil
	}
	return RoleImagePlatformOsValues[op]
}

var RoleImagePlatformOsValues = []any{"linux"}
var ValuesToRoleImagePlatformOs = map[any]RoleImagePlatformOs{
  RoleImagePlatformOsValues[RoleImagePlatformOsLinux]: RoleImagePlatformOsLinux,
}
