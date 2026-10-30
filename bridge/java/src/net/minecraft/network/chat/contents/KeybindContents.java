package net.minecraft.network.chat.contents;

import net.minecraft.network.chat.ComponentContents;

public record KeybindContents(String name) implements ComponentContents {
    public String getName() { return name; }
}
