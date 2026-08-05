
package generated

type RoleImagePlatformArchitecture uint

const (
  RoleImagePlatformArchitectureAmd64 RoleImagePlatformArchitecture = iota
  RoleImagePlatformArchitectureArm64
)

// Value returns the value of the enum.
func (op RoleImagePlatformArchitecture) Value() any {
	if op >= RoleImagePlatformArchitecture(len(RoleImagePlatformArchitectureValues)) {
		return nil
	}
	return RoleImagePlatformArchitectureValues[op]
}

var RoleImagePlatformArchitectureValues = []any{"amd64","arm64"}
var ValuesToRoleImagePlatformArchitecture = map[any]RoleImagePlatformArchitecture{
  RoleImagePlatformArchitectureValues[RoleImagePlatformArchitectureAmd64]: RoleImagePlatformArchitectureAmd64,
  RoleImagePlatformArchitectureValues[RoleImagePlatformArchitectureArm64]: RoleImagePlatformArchitectureArm64,
}
