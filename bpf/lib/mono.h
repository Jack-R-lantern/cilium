/* SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause) */
/* Copyright Authors of Cilium */

#pragma once

/* Moved out from time.h to avoid circular header dependency. */
#if defined(ENABLE_JIFFIES) && KERNEL_HZ != 1
# define BPF_MONO_SCALER	8
# define bpf_mono_now()		(jiffies >> BPF_MONO_SCALER)
# define bpf_sec_to_mono(s)	((__u32)bpf_sec_to_jiffies(s) >> BPF_MONO_SCALER)
#else
# define bpf_mono_now()		bpf_ktime_get_sec()
# define bpf_sec_to_mono(s)	(s)
#endif /* ENABLE_JIFFIES */
