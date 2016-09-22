/**
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"mynewt.apache.org/newt/newt/builder"
	"mynewt.apache.org/newt/newt/pkg"
	"mynewt.apache.org/newt/newt/project"
	"mynewt.apache.org/newt/newt/target"
	"mynewt.apache.org/newt/util"
)

const TARGET_TEST_NAME = "unittest"

var testablePkgMap map[*pkg.LocalPackage]struct{}

func testablePkgs() map[*pkg.LocalPackage]struct{} {
	if testablePkgMap != nil {
		return testablePkgMap
	}

	testablePkgMap := map[*pkg.LocalPackage]struct{}{}

	// Create a map of path => lclPkg.
	proj := project.GetProject()
	if proj == nil {
		return nil
	}

	allPkgs := proj.PackagesOfType(-1)
	pathLpkgMap := make(map[string]*pkg.LocalPackage, len(allPkgs))
	for _, p := range allPkgs {
		lpkg := p.(*pkg.LocalPackage)
		pathLpkgMap[lpkg.BasePath()] = lpkg
	}

	// Add all unit test packages to the testable package map.
	testPkgs := proj.PackagesOfType(pkg.PACKAGE_TYPE_UNITTEST)
	for _, p := range testPkgs {
		lclPack := p.(*pkg.LocalPackage)
		testablePkgMap[lclPack] = struct{}{}
	}

	// Next add first ancestor of each test package.
	for testPkg, _ := range testablePkgMap {
		for cur := filepath.Dir(testPkg.BasePath()); cur != proj.BasePath; cur = filepath.Dir(cur) {
			lpkg := pathLpkgMap[cur]
			if lpkg != nil && lpkg.Type() != pkg.PACKAGE_TYPE_UNITTEST {
				testablePkgMap[lpkg] = struct{}{}
				break
			}
		}
	}

	return testablePkgMap
}

func pkgToUnitTests(pack *pkg.LocalPackage) []*pkg.LocalPackage {
	// If the user specified a unittest package, just test that one.
	if pack.Type() == pkg.PACKAGE_TYPE_UNITTEST {
		return []*pkg.LocalPackage{pack}
	}

	// Otherwise, return all the package's direct descendants that are unit
	// test packages.
	result := []*pkg.LocalPackage{}
	srcPath := pack.BasePath()
	for p, _ := range testablePkgs() {
		if p.Type() == pkg.PACKAGE_TYPE_UNITTEST &&
			filepath.Dir(p.BasePath()) == srcPath {

			result = append(result, p)
		}
	}

	return result
}

var extraJtagCmd string

func buildRunCmd(cmd *cobra.Command, args []string) {
	if err := project.Initialize(); err != nil {
		NewtUsage(cmd, err)
	}
	if len(args) < 1 {
		NewtUsage(cmd, nil)
	}

	// Verify that all target names are valid.
	_, err := ResolveTargets(args...)
	if err != nil {
		NewtUsage(cmd, err)
	}

	for _, targetName := range args {
		// Reset the global state for the next build.
		if err := ResetGlobalState(); err != nil {
			NewtUsage(nil, err)
		}

		// Lookup the target by name.  This has to be done a second time here
		// now that the project has been reset.
		t := ResolveTarget(targetName)
		if t == nil {
			NewtUsage(nil, util.NewNewtError("Failed to resolve target: "+
				targetName))
		}

		util.StatusMessage(util.VERBOSITY_DEFAULT, "Building target %s\n",
			t.FullName())

		b, err := builder.NewTargetBuilder(t)
		if err != nil {
			NewtUsage(nil, err)
		}

		err = b.Build()
		if err != nil {
			NewtUsage(nil, err)
		}

		util.StatusMessage(util.VERBOSITY_DEFAULT, "Target successfully built: "+
			"%s\n", targetName)

		/* TODO */
	}
}

func cleanRunCmd(cmd *cobra.Command, args []string) {
	if err := project.Initialize(); err != nil {
		NewtUsage(cmd, err)
	}
	if len(args) < 1 {
		NewtUsage(cmd, util.NewNewtError("Must specify target"))
	}

	cleanAll := false
	targets := []*target.Target{}
	for _, arg := range args {
		if arg == TARGET_KEYWORD_ALL {
			cleanAll = true
		} else {
			t := ResolveTarget(arg)
			if t == nil {
				NewtUsage(cmd, util.NewNewtError("invalid target name: "+arg))
			}
			targets = append(targets, t)
		}
	}

	if cleanAll {
		path := builder.BinRoot()
		util.StatusMessage(util.VERBOSITY_VERBOSE,
			"Cleaning directory %s\n", path)

		err := os.RemoveAll(path)
		if err != nil {
			NewtUsage(cmd, err)
		}
	} else {
		for _, t := range targets {
			b, err := builder.NewTargetBuilder(t)
			if err != nil {
				NewtUsage(cmd, err)
			}
			err = b.Clean()
			if err != nil {
				NewtUsage(cmd, err)
			}
		}
	}
}

func pkgnames(pkgs []*pkg.LocalPackage) string {
	s := ""

	for _, p := range pkgs {
		s += p.Name() + " "
	}

	return s
}

func testRunCmd(cmd *cobra.Command, args []string) {
	if len(args) < 1 {
		NewtUsage(cmd, nil)
	}

	// Verify and resolve each specified package.
	testAll := false
	packs := []*pkg.LocalPackage{}
	for _, pkgName := range args {
		if pkgName == "all" {
			testAll = true
		} else {
			pack, err := ResolvePackage(pkgName)
			if err != nil {
				NewtUsage(cmd, err)
			}

			testPkgs := pkgToUnitTests(pack)
			if len(testPkgs) == 0 {
				NewtUsage(nil, util.FmtNewtError("Package %s contains no "+
					"unit tests", pack.FullName()))
			}

			packs = append(packs, testPkgs...)
		}
	}

	proj := project.GetProject()

	if testAll {
		packItfs := proj.PackagesOfType(pkg.PACKAGE_TYPE_UNITTEST)
		packs = make([]*pkg.LocalPackage, len(packItfs))
		for i, p := range packItfs {
			packs[i] = p.(*pkg.LocalPackage)
		}

		packs = pkg.SortLclPkgs(packs)
	}

	if len(packs) == 0 {
		NewtUsage(nil, util.NewNewtError("No testable packages found"))
	}

	passedPkgs := []*pkg.LocalPackage{}
	failedPkgs := []*pkg.LocalPackage{}
	for _, pack := range packs {
		// Reset the global state for the next test.
		if err := ResetGlobalState(); err != nil {
			NewtUsage(nil, err)
		}

		// Each unit test package gets its own target.  This target is a copy
		// of the base unit test package, just with an appropriate name.  The
		// reason each test needs a unique target is: syscfg and sysinit are
		// target-specific.  If each test package shares a target, they will
		// overwrite these generated headers each time they are run.  Worse, if
		// two tests are run back-to-back, the timestamps may indicate that the
		// headers have not changed between tests, causing build failures.
		baseTarget := ResolveTarget(TARGET_TEST_NAME)
		if baseTarget == nil {
			NewtUsage(nil, util.NewNewtError("Can't find unit test target: "+
				TARGET_TEST_NAME))
		}

		targetName := fmt.Sprintf("%s/%s/%s",
			TARGET_DEFAULT_DIR, TARGET_TEST_NAME,
			builder.TestTargetName(pack.Name()))

		t := ResolveTarget(targetName)
		if t == nil {
			targetName, err := ResolveNewTargetName(targetName)
			if err != nil {
				NewtUsage(nil, err)
			}

			t = baseTarget.Clone(proj.LocalRepo(), targetName)
			if err := t.Save(); err != nil {
				NewtUsage(nil, err)
			}
		}

		b, err := builder.NewTargetBuilder(t)
		if err != nil {
			NewtUsage(nil, err)
		}

		util.StatusMessage(util.VERBOSITY_DEFAULT, "Testing package %s\n",
			pack.FullName())

		// The package under test needs to be resolved again now that the
		// project has been reset.
		newPack, err := ResolvePackage(pack.FullName())
		if err != nil {
			NewtUsage(nil, util.NewNewtError("Failed to resolve package: "+
				pack.Name()))
		}
		pack = newPack

		err = b.Test(pack)
		if err == nil {
			passedPkgs = append(passedPkgs, pack)
		} else {
			newtError := err.(*util.NewtError)
			util.StatusMessage(util.VERBOSITY_QUIET, newtError.Text)
			failedPkgs = append(failedPkgs, pack)
		}
	}

	passStr := fmt.Sprintf("Passed tests: [%s]", PackageNameList(passedPkgs))
	failStr := fmt.Sprintf("Failed tests: [%s]", PackageNameList(failedPkgs))

	if len(failedPkgs) > 0 {
		NewtUsage(nil, util.FmtNewtError("Test failure(s):\n%s\n%s", passStr,
			failStr))
	} else {
		util.StatusMessage(util.VERBOSITY_DEFAULT, "%s\n", passStr)
		util.StatusMessage(util.VERBOSITY_DEFAULT, "All tests passed\n")
	}
}

func loadRunCmd(cmd *cobra.Command, args []string) {
	if err := project.Initialize(); err != nil {
		NewtUsage(cmd, err)
	}
	if len(args) < 1 {
		NewtUsage(cmd, util.NewNewtError("Must specify target"))
	}

	t := ResolveTarget(args[0])
	if t == nil {
		NewtUsage(cmd, util.NewNewtError("Invalid target name: "+args[0]))
	}

	b, err := builder.NewTargetBuilder(t)
	if err != nil {
		NewtUsage(cmd, err)
	}

	err = b.Load(extraJtagCmd)
	if err != nil {
		NewtUsage(cmd, err)
	}
}

func debugRunCmd(cmd *cobra.Command, args []string) {
	if err := project.Initialize(); err != nil {
		NewtUsage(cmd, err)
	}
	if len(args) < 1 {
		NewtUsage(cmd, util.NewNewtError("Must specify target"))
	}

	t := ResolveTarget(args[0])
	if t == nil {
		NewtUsage(cmd, util.NewNewtError("Invalid target name: "+args[0]))
	}

	b, err := builder.NewTargetBuilder(t)
	if err != nil {
		NewtUsage(cmd, err)
	}

	err = b.Debug(extraJtagCmd, false)
	if err != nil {
		NewtUsage(cmd, err)
	}
}

func sizeRunCmd(cmd *cobra.Command, args []string) {
	if err := project.Initialize(); err != nil {
		NewtUsage(cmd, err)
	}
	if len(args) < 1 {
		NewtUsage(cmd, util.NewNewtError("Must specify target"))
	}

	t := ResolveTarget(args[0])
	if t == nil {
		NewtUsage(cmd, util.NewNewtError("Invalid target name: "+args[0]))
	}

	b, err := builder.NewTargetBuilder(t)
	if err != nil {
		NewtUsage(cmd, err)
	}

	err = b.Size()
	if err != nil {
		NewtUsage(cmd, err)
	}
}

func AddBuildCommands(cmd *cobra.Command) {
	buildCmd := &cobra.Command{
		Use:   "build <target-name> [target-names...]",
		Short: "Builds one or more targets.",
		Run:   buildRunCmd,
	}

	buildCmd.ValidArgs = targetList()
	cmd.AddCommand(buildCmd)

	cleanCmd := &cobra.Command{
		Use:   "clean <target-name> [target-names...] | all",
		Short: "Deletes build artifacts for one or more targets.",
		Run:   cleanRunCmd,
	}

	cleanCmd.ValidArgs = append(targetList(), "all")
	cmd.AddCommand(cleanCmd)

	testCmd := &cobra.Command{
		Use:   "test <package-name> [package-names...] | all",
		Short: "Executes unit tests for one or more packages",
		Run:   testRunCmd,
	}
	testCmd.ValidArgs = append(packageList(), "all")
	cmd.AddCommand(testCmd)

	loadHelpText := "Load app image to target for <target-name>."

	loadCmd := &cobra.Command{
		Use:   "load <target-name>",
		Short: "Load built target to board",
		Long:  loadHelpText,
		Run:   loadRunCmd,
	}

	loadCmd.ValidArgs = targetList()
	cmd.AddCommand(loadCmd)
	loadCmd.PersistentFlags().StringVarP(&extraJtagCmd, "extrajtagcmd", "j", "",
		"extra commands to send to JTAG software")

	debugHelpText := "Open debugger session for <target-name>."

	debugCmd := &cobra.Command{
		Use:   "debug <target-name>",
		Short: "Open debugger session to target",
		Long:  debugHelpText,
		Run:   debugRunCmd,
	}

	debugCmd.ValidArgs = targetList()
	cmd.AddCommand(debugCmd)
	debugCmd.PersistentFlags().StringVarP(&extraJtagCmd, "extrajtagcmd", "j", "",
		"extra commands to send to JTAG software")

	sizeHelpText := "Calculate the size of target components specified by " +
		"<target-name>."

	sizeCmd := &cobra.Command{
		Use:   "size <target-name>",
		Short: "Size of target components",
		Long:  sizeHelpText,
		Run:   sizeRunCmd,
	}

	sizeCmd.ValidArgs = targetList()
	cmd.AddCommand(sizeCmd)

}
