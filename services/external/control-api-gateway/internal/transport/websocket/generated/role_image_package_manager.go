
package generated

type RoleImagePackageManager uint

const (
  RoleImagePackageManagerApk RoleImagePackageManager = iota
  RoleImagePackageManagerApt
  RoleImagePackageManagerDnf
  RoleImagePackageManagerPip
  RoleImagePackageManagerNpm
)

// Value returns the value of the enum.
func (op RoleImagePackageManager) Value() any {
	if op >= RoleImagePackageManager(len(RoleImagePackageManagerValues)) {
		return nil
	}
	return RoleImagePackageManagerValues[op]
}

var RoleImagePackageManagerValues = []any{"apk","apt","dnf","pip","npm"}
var ValuesToRoleImagePackageManager = map[any]RoleImagePackageManager{
  RoleImagePackageManagerValues[RoleImagePackageManagerApk]: RoleImagePackageManagerApk,
  RoleImagePackageManagerValues[RoleImagePackageManagerApt]: RoleImagePackageManagerApt,
  RoleImagePackageManagerValues[RoleImagePackageManagerDnf]: RoleImagePackageManagerDnf,
  RoleImagePackageManagerValues[RoleImagePackageManagerPip]: RoleImagePackageManagerPip,
  RoleImagePackageManagerValues[RoleImagePackageManagerNpm]: RoleImagePackageManagerNpm,
}
