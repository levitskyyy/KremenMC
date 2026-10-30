package net.minecraft.network.chat.numbers;

import net.minecraft.network.chat.Component;

public record FixedFormat(Component value) implements NumberFormat {}
