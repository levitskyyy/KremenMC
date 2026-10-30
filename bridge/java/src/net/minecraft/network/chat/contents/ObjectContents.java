package net.minecraft.network.chat.contents;

import net.minecraft.network.chat.ComponentContents;
import net.minecraft.network.chat.contents.objects.ObjectInfo;

public record ObjectContents(ObjectInfo contents) implements ComponentContents {}
