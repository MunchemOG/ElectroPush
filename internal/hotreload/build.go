package hotreload

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// source is the OpMode that gets compiled and pushed.
//
// It does nothing on purpose. The question is whether it appears on the Driver
// Station at all, and anything it did would only add ways for the experiment to
// fail for an unrelated reason.
const source = `package ` + Package + `;

import com.qualcomm.robotcore.eventloop.opmode.LinearOpMode;
import com.qualcomm.robotcore.eventloop.opmode.TeleOp;
import com.qualcomm.robotcore.hardware.DcMotor;

@TeleOp(name = "%s", group = "epsh")
public class ` + ClassName + ` extends LinearOpMode {

    private static final String MOTOR = "%s";

    @Override
    public void runOpMode() {
        // Binding is the point, so a missing motor has to be reported rather
        // than thrown: the question is whether a reloaded class can reach
        // hardwareMap at all, which a crash on init would not answer.
        DcMotor motor = null;
        String status;
        try {
            motor = hardwareMap.get(DcMotor.class, MOTOR);
            status = "bound";
        } catch (Throwable t) {
            status = t.getClass().getSimpleName() + ": " + t.getMessage();
        }

        telemetry.addData("motor", MOTOR);
        telemetry.addData("binding", status);
        if (motor != null) {
            telemetry.addData("encoder", motor.getCurrentPosition());
        }
        telemetry.addLine("Loaded from a pushed dex, without installing an APK.");
        telemetry.update();

        waitForStart();

        while (opModeIsActive()) {
            telemetry.addData("motor", MOTOR);
            telemetry.addData("binding", status);
            if (motor != null) {
                telemetry.addData("encoder", motor.getCurrentPosition());
            }
            telemetry.update();
            idle();
        }
    }
}
`

// built is what the robot needs. Two files, not one: the SDK discovers class
// names by opening the .jar files and listing .class entries, and loads the
// classes from the .dex. Push only the dex and the class loads fine but is
// never named, so no OpMode appears.
type built struct {
	Jar string
	Dex string
}

// buildDex compiles the OpMode and turns it into a dex.
func buildDex(tc Toolchain, work, opModeName, motor string) (string, error) {
	srcDir := filepath.Join(work, "src", filepath.FromSlash(strings.ReplaceAll(Package, ".", "/")))
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return "", err
	}

	srcFile := filepath.Join(srcDir, ClassName+".java")
	if err := os.WriteFile(srcFile, []byte(fmt.Sprintf(source, opModeName, motor)), 0o644); err != nil {
		return "", err
	}

	classes := filepath.Join(work, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		return "", err
	}

	// Java 8 bytecode: d8 accepts newer, but the hub's runtime is old enough
	// that staying where the FTC SDK is avoids a surprise.
	javac := exec.Command(tc.Javac,
		"-source", "8", "-target", "8",
		"-nowarn",
		"-classpath", strings.Join(tc.Jars, string(os.PathListSeparator)),
		"-d", classes,
		srcFile)

	if out, err := javac.CombinedOutput(); err != nil {
		return "", fmt.Errorf("compiling the OpMode failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	classFile := filepath.Join(classes, filepath.FromSlash(strings.ReplaceAll(Package, ".", "/")), ClassName+".class")

	dexDir := filepath.Join(work, "dex")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		return "", err
	}

	// --min-api 24 matches the FTC minSdk. Without it d8 targets something
	// older and emits desugaring the hub does not need.
	d8 := exec.Command(tc.D8,
		"--min-api", "24",
		"--output", dexDir,
		"--lib", tc.Jars[0],
		classFile)

	if out, err := d8.CombinedOutput(); err != nil {
		return "", fmt.Errorf("dexing failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	dex := filepath.Join(dexDir, "classes.dex")
	if _, err := os.Stat(dex); err != nil {
		return "", fmt.Errorf("d8 produced no classes.dex")
	}

	return dex, nil
}

// buildAll produces the jar and the dex the robot controller needs.
func buildAll(tc Toolchain, work, opModeName, motor string) (built, error) {
	dex, err := buildDex(tc, work, opModeName, motor)
	if err != nil {
		return built{}, err
	}

	entry := strings.ReplaceAll(Package, ".", "/") + "/" + ClassName + ".class"
	classFile := filepath.Join(work, "classes", filepath.FromSlash(entry))

	jar := filepath.Join(work, "epsh-reload-proof.jar")
	if err := writeJar(jar, entry, classFile); err != nil {
		return built{}, err
	}

	return built{Jar: jar, Dex: dex}, nil
}

// writeJar packs the compiled class under its package path. Only the entry
// names are read, but the real class goes in so the jar is not a lie.
func writeJar(jarPath, entry, classFile string) error {
	class, err := os.ReadFile(classFile)
	if err != nil {
		return fmt.Errorf("cannot read the compiled class: %w", err)
	}

	out, err := os.Create(jarPath)
	if err != nil {
		return err
	}
	defer out.Close()

	archive := zip.NewWriter(out)

	w, err := archive.Create(entry)
	if err != nil {
		return err
	}
	if _, err := w.Write(class); err != nil {
		return err
	}

	return archive.Close()
}

// classesJar pulls classes.jar out of an AAR into a temporary file, because
// javac cannot read an AAR.
func classesJar(aarPath string) (string, error) {
	archive, err := zip.OpenReader(aarPath)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", filepath.Base(aarPath), err)
	}
	defer archive.Close()

	for _, entry := range archive.File {
		if entry.Name != "classes.jar" {
			continue
		}

		reader, err := entry.Open()
		if err != nil {
			return "", err
		}
		defer reader.Close()

		out, err := os.CreateTemp("", "epsh-ftc-*.jar")
		if err != nil {
			return "", err
		}
		defer out.Close()

		if _, err := io.Copy(out, reader); err != nil {
			os.Remove(out.Name())
			return "", err
		}
		return out.Name(), nil
	}

	return "", fmt.Errorf("%s has no classes.jar", filepath.Base(aarPath))
}
