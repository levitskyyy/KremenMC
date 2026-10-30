package net.minecraft.network.chat.numbers;

import net.minecraft.network.chat.Style;

public record StyledFormat(Style style) implements NumberFormat {
    public static final StyledFormat NO_STYLE = new StyledFormat(Style.EMPTY);
}
