use std::fs::{File, OpenOptions};
use std::io::{Read, Seek, Write};
use std::sync::{Arc, Condvar, Mutex};
use std::thread;
use std::time::{Duration, Instant};

fn sleep_worker() {
    loop {
        thread::sleep(Duration::from_millis(15));
    }
}

fn lock_worker(lock: Arc<Mutex<u64>>) {
    loop {
        {
            let mut guard = lock.lock().expect("lock poisoned");
            *guard = guard.wrapping_add(1);
            thread::sleep(Duration::from_millis(8));
        }
        thread::yield_now();
    }
}

fn condvar_waiter(pair: Arc<(Mutex<bool>, Condvar)>) {
    let (lock, cvar) = &*pair;
    loop {
        let mut ready = lock.lock().expect("lock poisoned");
        while !*ready {
            ready = cvar.wait(ready).expect("condvar wait failed");
        }
        *ready = false;
    }
}

fn condvar_signaler(pair: Arc<(Mutex<bool>, Condvar)>) {
    let (lock, cvar) = &*pair;
    loop {
        thread::sleep(Duration::from_millis(20));
        let mut ready = lock.lock().expect("lock poisoned");
        *ready = true;
        cvar.notify_one();
    }
}

fn block_io_worker(path: &str) {
    let mut file = OpenOptions::new()
        .create(true)
        .truncate(true)
        .read(true)
        .write(true)
        .open(path)
        .expect("open block-io file failed");

    let block = vec![0x5a_u8; 1024 * 1024];
    let mut read_buf = vec![0_u8; 8192];
    loop {
        file.write_all(&block).expect("file write failed");
        file.sync_data().expect("sync_data failed");
        file.rewind().expect("rewind failed");
        let _ = file.read(&mut read_buf).expect("file read failed");
        thread::yield_now();
    }
}

fn alloc_worker() {
    let mut step = 0usize;
    loop {
        // Cycle allocation size to trigger allocator paths (small + large).
        let size = 64 * 1024 + (step % 64) * 64 * 1024;
        let mut v = Vec::with_capacity(size);
        v.resize(size, 0xaa);
        let sum: u64 = v.iter().map(|&x| x as u64).sum();
        if sum == 0 {
            eprintln!("unexpected");
        }
        step = step.wrapping_add(1);
        thread::sleep(Duration::from_millis(2));
    }
}

fn main() {
    println!("offcpu demo pid={}", std::process::id());

    let demo_file =
        std::env::var("OFFCPU_DEMO_FILE").unwrap_or_else(|_| "/tmp/offcpu-demo.bin".to_string());
    let syscall_log = std::env::var("OFFCPU_SYSCALL_LOG")
        .unwrap_or_else(|_| "/tmp/offcpu-syscall.log".to_string());
    let block_io_file = std::env::var("OFFCPU_BLOCK_IO_FILE").unwrap_or_else(|_| demo_file.clone());

    if let Err(err) = File::create(&block_io_file) {
        eprintln!("warn: create demo file {} failed: {}", block_io_file, err);
    }

    let mut handles = Vec::new();

    handles.push(thread::spawn(sleep_worker));

    let lock = Arc::new(Mutex::new(0_u64));
    handles.push(thread::spawn({
        let lock = Arc::clone(&lock);
        move || lock_worker(lock)
    }));
    handles.push(thread::spawn({
        let lock = Arc::clone(&lock);
        move || lock_worker(lock)
    }));

    let cond_pair = Arc::new((Mutex::new(false), Condvar::new()));
    handles.push(thread::spawn({
        let pair = Arc::clone(&cond_pair);
        move || condvar_waiter(pair)
    }));
    handles.push(thread::spawn({
        let pair = Arc::clone(&cond_pair);
        move || condvar_signaler(pair)
    }));

    handles.push(thread::spawn({
        let syscall_log = syscall_log.clone();
        move || {
            let mut src = File::open("/proc/self/stat").expect("open /proc/self/stat failed");
            let mut dst = OpenOptions::new()
                .create(true)
                .append(true)
                .open(&syscall_log)
                .expect("open syscall log failed");
            let mut buf = vec![0_u8; 4096];
            loop {
                src.rewind().expect("rewind /proc/self/stat failed");
                let n = src.read(&mut buf).expect("read /proc/self/stat failed");
                dst.write_all(&buf[..n]).expect("write syscall log failed");
                dst.write_all(b"\n").expect("write newline failed");
                dst.flush().expect("flush syscall log failed");
                thread::yield_now();
            }
        }
    }));

    handles.push(thread::spawn({
        let block_io_file = block_io_file.clone();
        move || block_io_worker(&block_io_file)
    }));
    handles.push(thread::spawn(alloc_worker));

    let start = Instant::now();
    loop {
        thread::sleep(Duration::from_secs(1));
        let secs = start.elapsed().as_secs();
        if secs % 5 == 0 {
            println!("demo running {}s ...", secs);
        }
    }
}
