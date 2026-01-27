import asyncio
from bleak import BleakClient, BleakScanner

# --- CONFIGURATION ---
ADDRESS = "C0:D6:3C:58:A4:10"
AUTH_PAYLOAD = b"HiLink"

# --- COMMANDS ---
CMD_HEARTBEAT = bytearray.fromhex("7E 00 01 03 00 92 00 00 9F 22 0D")
CMD_GET_DATA  = bytearray.fromhex("1E 00 01 03 00 8C 00 00 B1 44 0D")

UUID_NOTIFY = "0000fff1-0000-1000-8000-00805f9b34fb"
UUID_AUTH   = "0000fffa-0000-1000-8000-00805f9b34fb"
UUID_WRITE  = "0000fff2-0000-1000-8000-00805f9b34fb"

def decode_current_and_status(raw_val):
    # Top 2 bits are Status Flags
    # Bottom 14 bits are Current (0.1A scale)
    
    # Extract Status (Top 2 bits: 1100 0000 0000 0000)
    flags = (raw_val >> 14) & 0x03
    
    status_str = "Idle"
    if flags == 3: status_str = "Discharging" # 11 (C0)
    elif flags == 2: status_str = "Charging"  # 10 (80)
    elif flags == 1: status_str = "Protect"   # 01 (40)
    
    # Extract Current (Bottom 14 bits: 0011 1111 1111 1111)
    current_val = raw_val & 0x3FFF
    
    # Determine sign based on status
    amps = current_val / 10.0
    if status_str == "Discharging":
        amps = -amps
        
    return amps, status_str

def notification_handler(sender, data):
    if len(data) < 10: return
    if data[5] != 0x8C: return

    try:
        cursor = 8
        
        # 1. Cell Voltages
        num_cells = data[cursor]
        cursor += 1
        
        cell_volts = []
        for i in range(num_cells):
            mv = int.from_bytes(data[cursor:cursor+2], 'big')
            cell_volts.append(mv / 1000.0)
            cursor += 2
            
        # 2. Temperatures
        num_temps = data[cursor]
        cursor += 1
        
        temps = []
        for i in range(num_temps):
            raw_temp = int.from_bytes(data[cursor:cursor+2], 'big')
            temp_c = (raw_temp - 2731) / 10.0
            temps.append(round(temp_c, 1))
            cursor += 2
        
        # --- MAPPING ---
        
        # Slot A: Current + Status (The tricky one!)
        slot_a = int.from_bytes(data[cursor:cursor+2], 'big')
        current, status_str = decode_current_and_status(slot_a)
        cursor += 2
        
        # Slot B: Total Voltage
        volts_raw = int.from_bytes(data[cursor:cursor+2], 'big')
        total_voltage = volts_raw / 100.0
        cursor += 2
        
        # Slot C: Remaining Capacity
        rem_raw = int.from_bytes(data[cursor:cursor+2], 'big')
        rem_cap = rem_raw / 10.0
        cursor += 2
        
        # Slot D: Full Capacity
        full_raw = int.from_bytes(data[cursor:cursor+2], 'big')
        full_cap = full_raw / 10.0
        cursor += 2
        
        # Slot E: Cycles
        cycles = int.from_bytes(data[cursor:cursor+2], 'big')
        cursor += 2 
        
        # Slot F: Design Capacity
        design_raw = int.from_bytes(data[cursor:cursor+2], 'big')
        design_cap = design_raw / 10.0
        cursor += 2
        
        # Slot G: SOC
        soc = int.from_bytes(data[cursor:cursor+2], 'big')
        cursor += 2
        
        # --- OUTPUT ---
        print("\033[H\033[J", end="") # Clear Screen
        print("┌──────────────────────────────────────────┐")
        print(f"│  BATTERY STATUS             SOC: {soc:>3}%   │")
        print("├──────────────────────────────────────────┤")
        print(f"│  Voltage:  {total_voltage:>6.2f} V                    │")
        print(f"│  Current:  {current:>6.1f} A   ({status_str:<11})  │")
        print(f"│  Capacity: {rem_cap:>6.1f} / {full_cap:>5.1f} Ah         │")
        print(f"│  Design:   {design_cap:>6.1f} Ah                   │")
        print("├──────────────────────────────────────────┤")
        print("│  CELL VOLTAGES                           │")
        
        for i in range(0, len(cell_volts), 2):
            c1 = f"#{i+1}: {cell_volts[i]:.3f}V"
            c2 = f"#{i+2}: {cell_volts[i+1]:.3f}V" if i+1 < len(cell_volts) else ""
            print(f"│  {c1:<18} {c2:<18} │")

        print("├──────────────────────────────────────────┤")
        print(f"│  Temps: {str(temps):<31} │")
        print("└──────────────────────────────────────────┘")

    except Exception as e:
        print(f"Decoding Error: {e}")

async def run():
    print(f"Scanning for {ADDRESS}...")
    device = await BleakScanner.find_device_by_address(ADDRESS, timeout=10.0)
    
    if not device:
        print(f"Device {ADDRESS} not found.")
        return

    print(f"Connecting to {device.name}...")
    async with BleakClient(device) as client:
        print("Connected!")
        await client.start_notify(UUID_NOTIFY, notification_handler)
        
        await client.write_gatt_char(UUID_AUTH, AUTH_PAYLOAD, response=True)
        await asyncio.sleep(1.0)

        print("Starting Monitor...")
        while client.is_connected:
            await client.write_gatt_char(UUID_WRITE, CMD_HEARTBEAT, response=False)
            await asyncio.sleep(0.5)
            await client.write_gatt_char(UUID_WRITE, CMD_GET_DATA, response=False)
            await asyncio.sleep(1.0)

if __name__ == "__main__":
    loop = asyncio.get_event_loop()
    try:
        loop.run_until_complete(run())
    except KeyboardInterrupt:
        print("\nExiting...")