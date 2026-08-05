package robotcfg

// The tables here were read out of the FTC SDK 11.1.0 jars: the xmlTag on each
// driver's @MotorType / @ServoType / @AnalogSensorType / @DigitalIoDeviceType /
// @I2cDeviceType annotation, and the port counts in LynxConstants.
//
// They exist to catch two devices sharing a port. A tag that is not listed is
// left alone rather than guessed at, because teams register their own device
// types and a wrong guess would report a collision that is not there.

// Flavor is which set of ports on a hub a device occupies. Ports only collide
// within a flavor: a motor on port 0 and a servo on port 0 are both fine.
type Flavor int

const (
	// Unclassified is a tag this table does not know. Its ports are not
	// checked; third-party and OnBotJava drivers land here.
	Unclassified Flavor = iota
	Motor
	Servo
	Analog
	Digital
	I2C
	PWM
)

func (f Flavor) String() string {
	switch f {
	case Motor:
		return "motor"
	case Servo:
		return "servo"
	case Analog:
		return "analog"
	case Digital:
		return "digital"
	case I2C:
		return "I2C"
	case PWM:
		return "PWM"
	}
	return "device"
}

// Ports is how many of that flavor a single REV hub has, from LynxConstants.
func (f Flavor) Ports() int {
	switch f {
	case Motor:
		return 4
	case Servo:
		return 6
	case Analog:
		return 4
	case Digital:
		return 8
	case PWM:
		return 4
	}
	return 0
}

// Buses is how many I2C buses a single REV hub has.
const Buses = 4

// ControlHubAddress is the RS-485 address reserved for the Control Hub's
// built-in module. An Expansion Hub configured at this address is the error the
// SDK calls out by name.
const ControlHubAddress = 173

// MaxUnreservedAddress is the highest address a hub should be set to by hand.
// Above it the SDK warns that addresses are reserved for system use.
const MaxUnreservedAddress = 10

var flavors = map[string]Flavor{
	// Motors.
	"Motor":                               Motor,
	"Matrix12vMotor":                      Motor,
	"NeveRest20Gearmotor":                 Motor,
	"NeveRest3.7v1Gearmotor":              Motor,
	"NeveRest40Gearmotor":                 Motor,
	"NeveRest60Gearmotor":                 Motor,
	"RevRobotics20HDHexMotor":             Motor,
	"RevRobotics40HDHexMotor":             Motor,
	"RevRoboticsCoreHexMotor":             Motor,
	"RevRoboticsHDHexMotor":               Motor,
	"RevRoboticsUltraplanetaryHDHexMotor": Motor,
	"StudicaMaverick":                     Motor,
	"TetrixMotor":                         Motor,
	"goBILDA5201SeriesMotor":              Motor,
	"goBILDA5202SeriesMotor":              Motor,

	// Servos.
	"Servo":                   Servo,
	"ContinuousRotationServo": Servo,
	"RevSPARKMini":            Servo,
	"ServoFullRange":          Servo,

	// Analog inputs.
	"AnalogInput":                     Analog,
	"ModernRoboticsAnalogTouchSensor": Analog,
	"OpticalDistanceSensor":           Analog,

	// Digital I/O.
	"DigitalDevice":  Digital,
	"Led":            Digital,
	"RevTouchSensor": Digital,

	// I2C.
	"AdafruitBNO055IMU":              I2C,
	"AdafruitColorSensor":            I2C,
	"AndyMarkColor":                  I2C,
	"AndyMarkIMU":                    I2C,
	"AndyMarkTOF":                    I2C,
	"ColorSensor":                    I2C,
	"ControlHubImuBHI260AP":          I2C,
	"Gyro":                           I2C,
	"IrSeekerV3":                     I2C,
	"KauaiLabsNavxMicro":             I2C,
	"LynxColorSensor":                I2C,
	"LynxEmbeddedIMU":                I2C,
	"MaxSonarI2CXL":                  I2C,
	"ModernRoboticsI2cCompassSensor": I2C,
	"ModernRoboticsI2cRangeSensor":   I2C,
	"QWIIC_LED_STICK":                I2C,
	"REV_VL53L0X_RANGE_SENSOR":       I2C,
	"RevColorSensorV3":               I2C,
	"RevExternalImu":                 I2C,
	"SparkFunOTOS":                   I2C,
	"goBILDAPinpoint":                I2C,

	// PWM.
	"PulseWidthDevice": PWM,
}

// FlavorOf reports which ports a device type occupies.
func FlavorOf(tag string) Flavor {
	return flavors[tag]
}

// KnownTags lists every device type this table recognises. Only used to tell
// somebody what a near-miss might have meant.
func KnownTags() []string {
	tags := make([]string, 0, len(flavors))
	for tag := range flavors {
		tags = append(tags, tag)
	}
	return tags
}
