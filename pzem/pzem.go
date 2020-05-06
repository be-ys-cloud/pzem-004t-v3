package pzem

import (
	"fmt"
	"time"

	"github.com/dark705/pzem-004t-v3/crc16"

	"github.com/go-errors/errors"
	"github.com/tarm/serial"
)

type Register uint16
type Command uint8

const (
	//Voltage value 1LSB correspond to 0.1V
	Voltage Register = 0x0000
	//IntensitytLow 1LSB corrcspond to 0.001A
	IntensitytLow Register = 0x0001
	//IntensityHight 1LSB corrcspond to 0.001A
	IntensityHight Register = 0X0002
	//PowerLow 1LSB correspond to 0.1W
	PowerLow Register = 0x0003
	//PowerHigh 1LSB correspond to 0.1W
	PowerHigh Register = 0x0004
	//EnergyLow 1LSB correspond to 1Wh
	EnergyLow Register = 0x0005
	//EnergyHight 1LSB correspond to 1Wh
	EnergyHight Register = 0x0006
	//Frequency 1LSB correspond to 0.1Hz
	Frequency Register = 0x0007
	//PowerFactor 1lSB corresponcl to 0.01
	PowerFactor Register = 0x0008
	//Alarm status 0xFFFF  is alarm 0x0000 is not alarm
	Alarm Register = 0x0009

	//ModbusRTUAddress the range is 0x0001-0x00F7
	ModbusRTUAddress Register = 0x0002
	//AlarmThrhreshold  1LSB correspond to 1W
	AlarmThrhreshold Register = 0x0001

	//ReadHoldingRegister command
	ReadHoldingRegister Command = 0x03
	//ReadInputRegister command
	ReadInputRegister Command = 0X04
	//WriteSingleRegister command
	WriteSingleRegister Command = 0x06
	//Calibration command
	Calibration Command = 0x41
	//ResetEnergy command
	ResetEnergy Command = 0x42

	PzemUpdateTime            = 1000
	PzemDefaultBaudRate       = 9600
	PzemDefaultAddress  uint8 = 0xF8
)

//Probe is PZEM interface
type Probe interface {
	Voltage() (float32, error)
	Power() (float32, error)
	Energy() (float32, error)
	Frequency() (float32, error)
	Intensity() (float32, error)
	PowerFactor() (float32, error)
	ResetEnergy() error
}

// Config PZEM initialization
type Config struct {
	Port          string
	Speed         int
	SlaveArddress uint8
	TimeOut       time.Duration
}

type pzem struct {
	port        *serial.Port
	addr        uint8
	voltage     float32
	current     float32
	power       float32
	energy      float32
	frequeny    float32
	powerFactor float32
	alarms      uint16
	lastRead    time.Time
}

func debug(buf []uint8) {
	for _, v := range buf {
		fmt.Printf("%.2x", v)
	}
	fmt.Println()
}

//Setup initialize new PZEM device
func Setup(config Config) (Probe, error) {

	if config.Port == "" {
		return nil, errors.New("serial port must be set")
	}
	if config.Speed == 0 {
		config.Speed = PzemDefaultBaudRate
	}

	if config.SlaveArddress == 0 {
		config.SlaveArddress = PzemDefaultAddress
	}

	c := &serial.Config{Name: config.Port, Baud: config.Speed, ReadTimeout: config.TimeOut}
	s, err := serial.OpenPort(c)
	if err != nil {
		return nil, err
	}
	p := &pzem{port: s}
	p.initDevice(config.SlaveArddress)
	return p, nil
}

func (p *pzem) setSlaveArddress(addr uint8) error {
	if addr < 0x01 || addr > 0xF7 { // sanity check
		return errors.New("address provided is incorrect")
	}

	// Write the new address to the address register
	if err := p.sendCmd8(WriteSingleRegister, ModbusRTUAddress, uint16(addr), true); err != nil {
		return err
	}

	p.addr = addr // If successful, update the current slave address

	return nil
}

func (p *pzem) sendCmd8(cmd Command, reg Register, val uint16, check bool) error {
	var sendBuffer = make([]uint8, 8) // Send buffer
	var respBuffer = make([]uint8, 8) // Response buffer (only used when check is true)

	sendBuffer[0] = p.addr     // Set slave address
	sendBuffer[1] = uint8(cmd) // Set command

	sendBuffer[2] = uint8(reg>>8) & 0xFF // Set high byte of register address
	sendBuffer[3] = uint8(reg) & 0xFF    // Set low byte =//=

	sendBuffer[4] = uint8(val>>8) & 0xFF // Set high byte of register value
	sendBuffer[5] = uint8(val) & 0xFF    // Set low byte =//=

	setCRC(sendBuffer)

	n, err := p.port.Write([]byte(sendBuffer)) // send frame
	if n < len(sendBuffer) || err != nil {
		if err != nil {
			return err
		}
		return errors.Errorf("try to send %d, but %d sent", len(sendBuffer), n)
	}

	time.Sleep(200 * time.Millisecond)

	if check {
		if err := p.recieve(respBuffer); n <= 0 || err != nil { // if check enabled, read the response
			return err
		}

		// Check if response is same as send
		for i := 0; i < 8; i++ {
			if sendBuffer[i] != respBuffer[i] {
				return errors.New("response should be the same than the request")
			}
		}
	}

	return nil
}

func (p *pzem) initDevice(addr uint8) {
	if addr < 0x01 || addr > 0xF8 { // Sanity check of address
		p.addr = PzemDefaultAddress
	}
	p.addr = addr

	if p.addr != PzemDefaultAddress {
		p.setSlaveArddress(p.addr)
	}

}

func (p *pzem) updateValues() error {
	response := make([]uint8, 25)

	//If we read before the update time limit, do not update
	if p.lastRead.Add(PzemUpdateTime * time.Millisecond).After(time.Now()) {
		return nil
	}

	// Read 10 registers starting at 0x00 (no check)
	if err := p.sendCmd8(ReadInputRegister, 0x00, 0x0A, false); err != nil {
		return err
	}

	if err := p.recieve(response); err != nil { // Something went wrong
		return err
	}

	// Update the current values
	p.voltage = float32(uint32(response[3])<<8| // Raw voltage in 0.1V
		uint32(response[4])) / 10.0

	p.current = float32(uint32(response[5])<<8| // Raw current in 0.001A
		uint32(response[6])|
		uint32(response[7])<<24|
		uint32(response[8])<<16) / 1000.0

	p.power = float32(uint32(response[9])<<8| // Raw power in 0.1W
		uint32(response[10])|
		uint32(response[11])<<24|
		uint32(response[12])<<16) / 10.0

	p.energy = float32(uint32(response[13])<<8| // Raw Energy in 1Wh
		uint32(response[14])|
		uint32(response[15])<<24|
		uint32(response[16])<<16) / 1000.0

	p.frequeny = float32(uint32(response[17])<<8| // Raw Frequency in 0.1Hz
		uint32(response[18])) / 10.0

	p.powerFactor = float32(uint32(response[19])<<8| // Raw pf in 0.01
		uint32(response[20])) / 100.0

	p.alarms = uint16(uint32(response[21])<<8 | // Raw alarm value
		uint32(response[22]))

	p.lastRead = time.Now()

	return nil
}

func isError(buf []uint8) error {
	if buf[1] == 0x84 {
		switch buf[2] {
		case 0x01:
			return errors.New("Illegal command")
		case 0x02:
			return errors.New("Illegal address")
		case 0x03:
			return errors.New("Illegal data")
		case 0x04:
			return errors.New("Slave error")
		default:
			return errors.New("Unknown error")
		}

	}
	return nil
}

func (p *pzem) recieve(resp []uint8) error {
	n, err := p.port.Read(resp)
	if err != nil {
		return err
	}

	if n != len(resp) {
		return errors.Errorf("should got %d, but %d recieved", len(resp), n)
	}

	if !checkCRC(resp) {
		return errors.New("recieved CRC is not valid")
	}

	if err := isError(resp); err != nil {
		return err
	}

	return nil
}

func checkCRC(buf []uint8) bool {
	l := len(buf)
	if l <= 2 {
		return false
	}
	var crc uint16 = crc16.CRC(buf[:l-2])
	return (uint16(buf[l-2]) | uint16(buf[l-1])<<8) == crc
}

func setCRC(buf []uint8) {
	l := len(buf)
	if l <= 2 {
		return
	}
	var crc uint16 = crc16.CRC(buf[:l-2])
	buf[l-2] = uint8(crc) & 0xFF
	buf[l-1] = uint8(crc>>8) & 0xFF

}

func (p *pzem) ResetEnergy() error {
	buffer := []uint8{0x00, uint8(ResetEnergy), 0x00, 0x00}
	reply := make([]uint8, 4)
	buffer[0] = p.addr

	setCRC(buffer)

	p.port.Write(buffer)

	time.Sleep(400 * time.Millisecond)

	err := p.recieve(reply)
	if err != nil {
		return err
	}

	return nil
}

func (p *pzem) Voltage() (float32, error) {
	if err := p.updateValues(); err != nil {
		return 0.0, err
	}
	return p.voltage, nil
}

func (p *pzem) Intensity() (float32, error) {
	if err := p.updateValues(); err != nil {
		return 0.0, err
	}
	return p.current, nil
}

func (p *pzem) Power() (float32, error) {
	if err := p.updateValues(); err != nil {
		return 0.0, err
	}
	return p.power, nil
}

func (p *pzem) Energy() (float32, error) {
	if err := p.updateValues(); err != nil {
		return 0.0, err
	}
	return p.energy, nil
}

func (p *pzem) Frequency() (float32, error) {
	if err := p.updateValues(); err != nil {
		return 0.0, err
	}
	return p.frequeny, nil
}

func (p *pzem) PowerFactor() (float32, error) {
	if err := p.updateValues(); err != nil {
		return 0.0, err
	}
	return p.powerFactor, nil
}
