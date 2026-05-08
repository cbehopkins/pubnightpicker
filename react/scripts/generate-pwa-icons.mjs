import path from 'node:path';
import { fileURLToPath } from 'node:url';
import sharp from 'sharp';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const publicDir = path.resolve(__dirname, '..', 'public');
const sourcePath = path.join(publicDir, 'app_logo.png');

const iconSizes = [192, 512];
const maskableBackground = '#f4efe4';

async function createCleanLogoBuffer() {
    const source = sharp(sourcePath).ensureAlpha();
    const { data, info } = await source.raw().toBuffer({ resolveWithObject: true });

    for (let i = 0; i < data.length; i += 4) {
        const r = data[i];
        const g = data[i + 1];
        const b = data[i + 2];
        const a = data[i + 3];

        if (a > 0 && r < 30 && g < 30 && b < 30) {
            data[i + 3] = 0;
        }
    }

    return sharp(data, {
        raw: {
            width: info.width,
            height: info.height,
            channels: info.channels,
        },
    })
        .trim({ threshold: 8 })
        .png()
        .toBuffer();
}

async function writeLauncherIcons(cleanLogoBuffer) {
    for (const size of iconSizes) {
        const safeLogoSize = Math.round(size * 0.66);

        await sharp({
            create: {
                width: size,
                height: size,
                channels: 4,
                background: maskableBackground,
            },
        })
            .composite([
                {
                    input: await sharp(cleanLogoBuffer)
                        .resize({ width: safeLogoSize, height: safeLogoSize, fit: 'inside' })
                        .png()
                        .toBuffer(),
                    gravity: 'center',
                },
            ])
            .png()
            .toFile(path.join(publicDir, `icon-app-${size}-v2.png`));
    }
}

async function main() {
    const cleanLogoBuffer = await createCleanLogoBuffer();
    await writeLauncherIcons(cleanLogoBuffer);
    process.stdout.write('Generated icon-app-192-v2 and icon-app-512-v2 in public/.\n');
}

main().catch((error) => {
    console.error(error);
    process.exitCode = 1;
});
