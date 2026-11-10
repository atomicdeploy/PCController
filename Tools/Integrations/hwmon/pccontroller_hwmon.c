// SPDX-License-Identifier: GPL-2.0-only
/* Read-only hwmon facade fed by PCController's existing serial owner.
 * Each open file owns one device. Closing it removes that device. No USB,
 * I2C or output commands are issued here. Never used for thermal safety.
 */
#include <linux/module.h>
#include <linux/hwmon.h>
#include <linux/miscdevice.h>
#include <linux/fs.h>
#include <linux/slab.h>
#include <linux/uaccess.h>
#include <linux/mutex.h>
#include <linux/ktime.h>
#include <linux/kernel.h>
#include <linux/string.h>

struct bridge_state {
	struct device *hwmon;
	struct mutex lock;
	u64 expires;
	u32 valid;
	long value[6]; /* supply mV, bus mV, mA, uW, lighting mC, audio mC */
};
static struct miscdevice bridge_device;

static int bridge_index(enum hwmon_sensor_types type, int channel)
{
	switch (type) {
	case hwmon_in: return channel < 2 ? channel : -EINVAL;
	case hwmon_curr: return 2;
	case hwmon_power: return 3;
	case hwmon_temp: return 4 + channel;
	default: return -EINVAL;
	}
}

static umode_t bridge_visible(const void *data, enum hwmon_sensor_types type,
			     u32 attr, int channel)
{
	return 0444; /* Only attributes declared in bridge_info are instantiated. */
}

static int bridge_read(struct device *dev, enum hwmon_sensor_types type,
		       u32 attr, int channel, long *value)
{
	struct bridge_state *s = dev_get_drvdata(dev);
	int index = bridge_index(type, channel), ret = 0;
	bool valid;
	if (index < 0 || index >= 6)
		return -EINVAL;
	mutex_lock(&s->lock);
	valid = (s->valid & BIT(index)) && ktime_get_boottime_ns() < s->expires;
	if (type == hwmon_temp && attr == hwmon_temp_fault)
		*value = !valid;
	else if (!valid)
		ret = -ENODATA;
	else
		*value = s->value[index];
	mutex_unlock(&s->lock);
	return ret;
}

static int bridge_label(struct device *dev, enum hwmon_sensor_types type,
			u32 attr, int channel, const char **text)
{
	static const char * const labels[] = {
		"PCB supply", "INA219 bus", "PCB current", "PCB power",
		"Lighting DS18B20", "Audio DS18B20"
	};
	int index = bridge_index(type, channel);
	if (index < 0 || index >= ARRAY_SIZE(labels))
		return -EINVAL;
	*text = labels[index];
	return 0;
}

static const struct hwmon_ops bridge_ops = {
	.is_visible = bridge_visible, .read = bridge_read, .read_string = bridge_label,
};
static const struct hwmon_channel_info * const bridge_channels[] = {
	HWMON_CHANNEL_INFO(in, HWMON_I_INPUT | HWMON_I_LABEL, HWMON_I_INPUT | HWMON_I_LABEL),
	HWMON_CHANNEL_INFO(curr, HWMON_C_INPUT | HWMON_C_LABEL),
	HWMON_CHANNEL_INFO(power, HWMON_P_INPUT | HWMON_P_LABEL),
	HWMON_CHANNEL_INFO(temp, HWMON_T_INPUT | HWMON_T_LABEL | HWMON_T_FAULT,
			   HWMON_T_INPUT | HWMON_T_LABEL | HWMON_T_FAULT),
	NULL
};
static const struct hwmon_chip_info bridge_info = { .ops = &bridge_ops, .info = bridge_channels };

static int bridge_open(struct inode *inode, struct file *file)
{
	struct bridge_state *s = kzalloc(sizeof(*s), GFP_KERNEL);
	if (!s)
		return -ENOMEM;
	mutex_init(&s->lock);
	s->hwmon = hwmon_device_register_with_info(bridge_device.this_device,
				"pccontroller", s, &bridge_info, NULL);
	if (IS_ERR(s->hwmon)) {
		int ret = PTR_ERR(s->hwmon);
		kfree(s);
		return ret;
	}
	file->private_data = s;
	return nonseekable_open(inode, file);
}

static ssize_t bridge_write(struct file *file, const char __user *buffer,
			    size_t count, loff_t *offset)
{
	struct bridge_state *s = file->private_data;
	char text[192], *cursor, *word;
	unsigned int ttl, mask;
	long long fields[9], *v = fields + 3;
	int i;
	if (!count || count >= sizeof(text))
		return -EINVAL;
	if (copy_from_user(text, buffer, count))
		return -EFAULT;
	text[count] = 0;
	if (memchr(text, 0, count))
		return -EINVAL;
	cursor = text;
	i = 0;
	while ((word = strsep(&cursor, " \t\n")) != NULL) {
		if (!*word)
			continue;
		if (i == ARRAY_SIZE(fields) || kstrtoll(word, 10, &fields[i]))
			return -EINVAL;
		i++;
	}
	if (i != ARRAY_SIZE(fields) || fields[0] != 1 ||
	    fields[1] < 0 || fields[1] > 5000 || fields[2] < 0 || fields[2] > 63)
		return -EINVAL;
	ttl = fields[1];
	mask = fields[2];
	for (i = 0; i < 6; i++)
		if (v[i] < LONG_MIN || v[i] > LONG_MAX)
			return -ERANGE;
	if (v[0] < 0 || v[0] > 100000 || v[1] < 0 || v[1] > 100000 ||
	    v[2] < -100000 || v[2] > 100000 || v[3] < -10000000000LL || v[3] > 10000000000LL ||
	    v[4] < -55000 || v[4] > 125000 || v[5] < -55000 || v[5] > 125000)
		return -ERANGE;
	mutex_lock(&s->lock);
	for (i = 0; i < 6; i++)
		s->value[i] = v[i];
	s->valid = ttl ? mask : 0;
	s->expires = ktime_get_boottime_ns() + (u64)ttl * NSEC_PER_MSEC;
	mutex_unlock(&s->lock);
	return count;
}

static int bridge_release(struct inode *inode, struct file *file)
{
	struct bridge_state *s = file->private_data;
	hwmon_device_unregister(s->hwmon);
	kfree(s);
	return 0;
}
static const struct file_operations bridge_fops = {
	.owner = THIS_MODULE, .open = bridge_open, .write = bridge_write,
	.release = bridge_release, .llseek = no_llseek,
};
static struct miscdevice bridge_device = {
	.minor = MISC_DYNAMIC_MINOR, .name = "pccontroller-hwmon",
	.fops = &bridge_fops, .mode = 0600,
};
static int __init bridge_init(void) { return misc_register(&bridge_device); }
static void __exit bridge_exit(void) { misc_deregister(&bridge_device); }
module_init(bridge_init);
module_exit(bridge_exit);
MODULE_LICENSE("GPL");
MODULE_DESCRIPTION("PCController observed board telemetry hwmon facade");
