#![no_std]

use core::{mem::size_of, panic::PanicInfo, ptr, slice};

#[panic_handler]
fn panic(_info: &PanicInfo<'_>) -> ! {
    loop {}
}

#[unsafe(no_mangle)]
pub extern "C" fn tee_main(
    input_ptr: *const u8,
    input_len: usize,
    output_ptr: *mut u8,
    output_len: usize,
) -> u32 {
    if input_ptr.is_null() || output_ptr.is_null() {
        return u32::MAX;
    }
    if input_len != size_of::<i64>() * 2 || output_len < size_of::<i64>() {
        return u32::MAX;
    }

    let input = unsafe { slice::from_raw_parts(input_ptr, input_len) };
    let a = match read_i64(&input[..8]) {
        Some(value) => value,
        None => return u32::MAX,
    };
    let b = match read_i64(&input[8..16]) {
        Some(value) => value,
        None => return u32::MAX,
    };
    let sum = match a.checked_add(b) {
        Some(value) => value,
        None => return u32::MAX,
    };

    let sum_bytes = sum.to_le_bytes();
    unsafe { ptr::copy_nonoverlapping(sum_bytes.as_ptr(), output_ptr, sum_bytes.len()) };
    sum_bytes.len() as u32
}

fn read_i64(bytes: &[u8]) -> Option<i64> {
    if bytes.len() != size_of::<i64>() {
        return None;
    }
    let mut array = [0_u8; 8];
    array.copy_from_slice(bytes);
    Some(i64::from_le_bytes(array))
}
