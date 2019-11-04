package pzem

import (
	"time"

	"github.com/sigurn/crc16"
	"github.com/tarm/serial"
)

type Register uint16
type Command uint8

const (
	Voltage     Register = 0x0000
	CurrentL    Register = 0x0001
	CurrentH    Register = 0X0002
	PowerL      Register = 0x0003
	PowerH      Register = 0x0004
	EnergyL     Register = 0x0005
	EnergyH     Register = 0x0006
	Frequency   Register = 0x0007
	PowerFactor Register = 0x0008
	Alarm       Register = 0x0009

	WriteAddress  Register = 0x0002
	WriteAlarmThr Register = 0x0001

	RHR  Command = 0x03
	RIR  Command = 0X04
	WSR  Command = 0x06
	CAL  Command = 0x41
	REST Command = 0x42

	PzemUpdateTime     = 200
	PzemResponseSize   = 32
	PzemReadTimeout    = 100
	PzemBaudRate       = 9600
	PzemDefaultAddress = 0xF8
)

//Probe is PZEM interface
type Probe interface {
	Voltage() float32
	Power() float32
	Energy() float32
	Frequency() float32
	PowerFactor() float32
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

var table *crc16.Table

func init() {
	table = crc16.MakeTable(crc16.CRC16_MODBUS)
}

//Setup initialize new PZEM device
func Setup(port string, baud int) (*pzem, error) {
	c := &serial.Config{Name: port, Baud: baud}
	s, err := serial.OpenPort(c)
	if err != nil {
		return nil, err
	}
	return &pzem{port: s}, nil
}

/*!
 * PZEM004Tv30::setAddress
 *
 * Set a new device address and update the device
 * WARNING - should be used to set up devices once.
 * Code initializtion will still have old address on next run!
 *
 * @param[in] addr New device address 0x01-0xF7
 *
 * @return success
 */
func (p *pzem) setAddress(addr uint8) bool {
	if addr < 0x01 || addr > 0xF7 { // sanity check
		return false
	}

	// Write the new address to the address register
	if !p.sendCmd8(WSR, WriteAddress, uint16(addr), true) {
		return false
	}

	p.addr = addr // If successful, update the current slave address

	return true
}

/*!
 * PZEM004Tv30::sendCmd8
 *
 * Prepares the 8 byte command buffer and sends
 *
 * @param[in] cmd - Command to send (position 1)
 * @param[in] rAddr - Register address (postion 2-3)
 * @param[in] val - Register value to write (positon 4-5)
 * @param[in] check - perform a simple read check after write
 *
 * @return success
 */
func (p *pzem) sendCmd8(cmd Command, reg Register, val uint16, check bool) bool {
	var sendBuffer = make([]uint8, 8) // Send buffer
	var respBuffer = make([]uint8, 8) // Response buffer (only used when check is true)

	sendBuffer[0] = p.addr     // Set slave address
	sendBuffer[1] = uint8(cmd) // Set command

	sendBuffer[2] = uint8(reg>>8) & 0xFF // Set high byte of register address
	sendBuffer[3] = uint8(reg) & 0xFF    // Set low byte =//=

	sendBuffer[4] = uint8(val>>8) & 0xFF // Set high byte of register value
	sendBuffer[5] = uint8(val) & 0xFF    // Set low byte =//=

	//setCRC(sendBuffer, 8) // Set CRC of frame

	p.port.Write([]byte(sendBuffer)) // send frame

	if check {
		if n, err := p.recieve(respBuffer); n <= 0 || err != nil { // if check enabled, read the response
			return false
		}

		// Check if response is same as send
		for i := 0; i < 8; i++ {
			if sendBuffer[i] != respBuffer[i] {
				return false
			}
		}
	}
	return true
}

/*!
 * PZEM004Tv30::init
 *
 * initialization common to all consturctors
 *
 * @param[in] addr - device address
 *
 * @return success
 */
func (p *pzem) initDevice(addr uint8) {
	if addr < 0x01 || addr > 0xF8 { // Sanity check of address
		p.addr = PzemDefaultAddress
	}
	p.addr = addr

	// Set initial lastRed time so that we read right away
	p.lastRead = time.Now()
}

func (p *pzem) updateValues() bool {
	response := make([]uint8, 25)

	//If we read before the update time limit, do not update
	if p.lastRead.Add(PzemUpdateTime * time.Millisecond).After(time.Now()) {
		return true
	}

	// Read 10 registers starting at 0x00 (no check)
	p.sendCmd8(RIR, 0x00, 0x0A, false)

	if n, err := p.recieve(response); n != 25 || err != nil { // Something went wrong
		return false
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

	return true
}

func (p *pzem) recieve(resp []uint8) (uint16, error) {
	n, err := p.port.Read(resp)
	if err != nil {
		return 0, err
	}
	return uint16(n), nil
}

func checkCRC(buf []uint8) bool {
	l := len(buf)
	if l <= 2 {
		return false
	}
	// Compute CRC of data
	var crc uint16 = crc16.Checksum(buf[:l-2], table)
	return (uint16(buf[l-2]) | uint16(buf[l-1])<<8) == crc
}
